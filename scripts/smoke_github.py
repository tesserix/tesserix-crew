#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Opt-in real GitHub write/recovery acceptance with isolated scripted stage agents.

Creates two labelled fixture issues in the explicitly selected repository and
closes them only after executable checks and delivery gates pass.
"""
import argparse, json, os, pathlib, re, shutil, subprocess, sys, tempfile, time
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--crew',required=True)
parser.add_argument('--github-repo',required=True,help='Repository authorized for fixture issue creation/closure (owner/repo)')
options=parser.parse_args()
if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+',options.github_repo):parser.error('github-repo must be owner/repo')
if shutil.which('gh') is None:parser.error('gh must be installed and signed in')
root=pathlib.Path(tempfile.mkdtemp(prefix='crew-github-live-'))
repo,home,bin=root/'repo',root/'data',root/'bin'
for p in (repo,home,bin):p.mkdir()
(repo/'.crew').mkdir()
crew=str(pathlib.Path(options.crew).resolve()); real_gh=shutil.which('gh'); target=options.github_repo
(repo/'arithmetic.py').write_text('def add(a,b):\n    return a-b\n')
(repo/'test_unit.py').write_text('import unittest\nfrom arithmetic import add\nclass ArithmeticTests(unittest.TestCase):\n    def test_positive(self): self.assertEqual(add(2,3),5)\n    def test_negative(self): self.assertEqual(add(-2,-3),-5)\n')
suffix=root.name.rsplit('-',1)[-1]
proposals=[
 dict(title=f'[Crew acceptance {suffix}] Arithmetic implementation',body='Controlled GitHub lifecycle acceptance fixture. Work is isolated in a temporary repository; this is not a product bug report.',acceptance=['Pinned arithmetic unit checks pass after red/green implementation'],key='arithmetic'),
 dict(title=f'[Crew acceptance {suffix}] Independent validation',body='Controlled dependent GitHub acceptance fixture. Close only after independent validation and delivery evidence.',acceptance=['Independent checks and delivery gates pass'],key='validation',depends_on=['arithmetic'])]
adapter=home/'adapter.py'
adapter.write_text('''import json,os,pathlib,subprocess,sys
sys.stdin.read()
role=sys.argv[1];repo=pathlib.Path(os.environ['CREW_GH_REPO']);home=os.environ['CREW_GH_HOME'];id=os.environ['CREW_GH_ID'];crew=os.environ['CREW_GH_CLI']
def check(phase):
 p=subprocess.run([crew,'workflow','check','--repo',str(repo),'--home',home,'--name','unit','--phase',phase,'--json',id],capture_output=True,text=True)
 return p.returncode,p.stdout+p.stderr
state='pass';text='Controlled fixture '+role+' completed'
if role=='planner': text='```crew-issues\\n'+os.environ['CREW_GH_PROPOSALS']+'\\n```'
elif role=='maker':
 code,out=check('red')
 if code:state='blocked';text=out
 else:
  (repo/'arithmetic.py').write_text('def add(a,b):\\n    return a+b\\n')
  code,out=check('green');state='pass' if code==0 else 'fail';text='Recorded executable unit red/green. '+out
elif role=='checker':
 code,out=check('green');state='pass' if code==0 else 'fail';text='Independent fixture check. '+out
print(json.dumps({'type':'text','text':text+'\\nCREW_STAGE_RESULT: '+state}))
''')
wrapper=bin/'gh'
wrapper.write_text('#!'+sys.executable+'\n'+'''import json,os,pathlib,subprocess,sys
root=pathlib.Path(os.environ['CREW_GH_ROOT']);args=sys.argv[1:]
p=subprocess.run([os.environ['CREW_GH_REAL'],*args],capture_output=True)
with (root/'gh-calls.jsonl').open('a') as f:f.write(json.dumps({'operation':args[:2],'exit':p.returncode})+'\\n')
output=p.stdout
for operation in ['create','close']:
 flag=root/('lost-'+operation)
 if args[:2]==['issue',operation] and p.returncode==0 and not flag.exists():
  flag.write_text('Simulated lost response after successful real GitHub '+operation)
  (root/(operation+'-receipt')).write_bytes(p.stdout)
  sys.stderr.write('Controlled lost response after GitHub '+operation+'\\n');sys.exit(1)
if p.returncode==0 and args and args[0]=='api' and any('/issues?state=all&' in arg for arg in args):
 data=p.stdout.decode();decoder=json.JSONDecoder();pages=[]
 while data.strip():
  data=data.lstrip();page,n=decoder.raw_decode(data);pages.append(page);data=data[n:]
 for operation in ['create','close']:
  stale=root/('stale-'+operation)
  if (root/('lost-'+operation)).exists() and not stale.exists():
   if operation=='create':
    receipt=(root/'create-receipt').read_text().strip()
    pages=[[item for item in page if item.get('html_url')!=receipt] for page in pages]
   else:
    for page in pages:
     for item in page:
      if item.get('state')=='closed' and '<!-- crew-delivery:' not in (item.get('body') or '') and (root/'workflow-id').read_text() in (item.get('body') or ''):
       item['state']='open'
   stale.write_text('One deliberately stale metadata response')
 output=('\\n'.join(json.dumps(page) for page in pages)).encode()
sys.stdout.buffer.write(output);sys.stderr.buffer.write(p.stderr);sys.exit(p.returncode)
''');wrapper.chmod(0o700)
config='default_agent="planner"\n'
for name in ['designer','planner','reviewer','maker','checker','reporter']:
 config+=f'\n[providers.{name}]\nkind="cli"\ncommand={json.dumps([sys.executable,str(adapter),name])}\nformat="crew"\nauth="none"\nread_only_args=["--read"]\nedit_args=["--write"]\ncapabilities=["read","write","tools"]\n'
config+='\n[lifecycles.github-live]\ndescription="Real GitHub lifecycle and lost-response acceptance"\ngithub_repo="'+target+'"\ntdd=true\nmax_repairs=1\n[[lifecycles.github-live.checks]]\nname="unit"\nkind="unit"\ncommand='+json.dumps([sys.executable,'-m','unittest','test_unit.py'])+'\n'
stages=[('design','agent','designer','designer',False),('plan','agent','planner','planner',False),('agent-review','agent','reviewer','reviewer',False),('create-github-issues','github-create','','',False),('user-review','approval','','',False),('implement','agent','implementer','maker',True),('test','agent','tester','checker',True),('deliver','agent','deliverer','reporter',False),('close-github-issues','github-close','','',False)]
for name,kind,role,agent,edits in stages:
 config+='\n[[lifecycles.github-live.stages]]\nname='+json.dumps(name)+'\nkind='+json.dumps(kind)+'\nprompt="Execute only the controlled acceptance fixture stage"\n'
 if agent:config+='role='+json.dumps(role)+'\nagent='+json.dumps(agent)+'\nallow_edits='+str(edits).lower()+'\n'
(repo/'.crew'/'config.toml').write_text(config)
subprocess.run(['git','init','-q',str(repo)],check=True)
env=dict(os.environ,PATH=str(bin)+os.pathsep+os.environ['PATH'],CREW_HOME=str(home),PYTHONDONTWRITEBYTECODE='1',GH_PROMPT_DISABLED='1',CREW_GH_ROOT=str(root),CREW_GH_REAL=real_gh,CREW_GH_REPO=str(repo),CREW_GH_HOME=str(home),CREW_GH_CLI=crew,CREW_GH_PROPOSALS=json.dumps(proposals))
timeline=[]
def call(action,*args,expected=None):
 p=subprocess.run([crew,'workflow',action,'--repo',str(repo),'--home',str(home),'--json',*args],env=env,capture_output=True,text=True,timeout=120)
 (root/f'{len(timeline):02d}-{action}.stdout').write_text(p.stdout);(root/f'{len(timeline):02d}-{action}.stderr').write_text(p.stderr)
 w=json.loads(p.stdout);st=w.get('state') if isinstance(w,dict) else None
 timeline.append(dict(action=action,state=st,exit=p.returncode));(root/'timeline.json').write_text(json.dumps(timeline,indent=2));print(json.dumps(timeline[-1]),flush=True)
 if expected is not None and st not in expected:raise RuntimeError(f'Unexpected {st}; inspect {root}')
 return w
def remote(number):return json.loads(subprocess.check_output([real_gh,'issue','view',str(number),'--repo',target,'--json','state,body,comments,url'],text=True))
print('ROOT='+str(root),flush=True)
w=call('start','--preset','github-live','Controlled real GitHub lifecycle acceptance',expected=['ready']);id=w['id'];env['CREW_GH_ID']=id
(root/'workflow-id').write_text(id)
(root/'state.json').write_text(json.dumps(dict(root=str(root),repo=str(repo),home=str(home),id=id,target=target)))
for _ in range(3):w=call('next',id,expected=['ready'])
w=call('next',id,expected=['failed']);assert (root/'lost-create').exists()
w=call('retry','--note','Real creation succeeded but its response was lost; reconcile durable markers.',id,expected=['ready'])
w=call('next',id,expected=['failed']);assert w['issues'][0]['creation_pending'] and (root/'stale-create').exists()
for attempt in range(6):
 w=call('retry','--note','Reconcile the reserved creation intent without repeating writes.',id,expected=['ready'])
 w=call('next',id,expected=['failed','awaiting-approval'])
 if w['state']=='awaiting-approval':break
 time.sleep(1)
assert w['state']=='awaiting-approval' and len(w['issues'])==2
links=w['issues'];(root/'tracked-issues.json').write_text(json.dumps(links,indent=2))
assert links[0]['url'] in remote(links[1]['number'])['body']
assert w['session'] in remote(links[0]['number'])['body']
w=call('next','--allow-edits',id,expected=['awaiting-approval']);assert 'a-b' in (repo/'arithmetic.py').read_text()
w=call('approve','--note','Reviewed controlled fixture issue set, dependency and pinned unit command.',id,expected=['ready'])
w=call('next','--allow-edits',id,expected=['ready'])
(repo/'arithmetic.py').write_text('def add(a,b):\n    return a-b\n')
w=call('next','--allow-edits',id,expected=['failed'])
assert all(remote(link['number'])['state']=='OPEN' for link in links)
w=call('repair','--note','Seeded arithmetic regression reproduced; repair with fresh red/green evidence.',id,expected=['ready'])
for _ in range(3):w=call('next','--allow-edits',id,expected=['ready'])
w=call('next',id,expected=['failed']);assert (root/'lost-close').exists()
w=call('retry','--note','Real issue closure succeeded but response was lost; reconcile remote state before further writes.',id,expected=['ready'])
w=call('next',id,expected=['failed']);assert any(x.get('closure_pending') for x in w['issues']) and (root/'stale-close').exists()
for attempt in range(6):
 w=call('retry','--note','Reconcile closure intent without another comment or close write.',id,expected=['ready'])
 w=call('next',id,expected=['failed','completed'])
 if w['state']=='completed':break
 time.sleep(1)
assert w['state']=='completed' and all(x['closed'] for x in w['issues'])
for link in links:
 data=remote(link['number']);assert data['state']=='CLOSED'
 assert sum(('<!-- crew-delivery:'+id+' -->') in c['body'] for c in data['comments'])==1
before=(root/'gh-calls.jsonl').read_text();call('next',id,expected=['completed']);assert before==(root/'gh-calls.jsonl').read_text()
calls=[json.loads(x) for x in before.splitlines()]
assert sum(x['operation']==['issue','create'] for x in calls)==2
report=dict(state='passed',root=str(root),workflow=id,issues=[x['url'] for x in links],creation_calls=2,delivery_comments_per_issue=1,lost_create_reconciled=True,lost_close_reconciled=True,stale_metadata_refused_duplicate_writes=True,failed_checks_kept_issues_open=True,limitations=['Scripted stage agents; real gh authentication/API and real executable checks','Lost responses and one stale listing per operation are deliberately injected'])
(root/'result.json').write_text(json.dumps(report,indent=2));print(json.dumps(report),flush=True)
