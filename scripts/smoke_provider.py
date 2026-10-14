#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Opt-in external CLI provider/resume/shared-context acceptance with live models."""
import argparse, json, os, pathlib, re, sqlite3, subprocess, tempfile
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--crew',required=True)
options=parser.parse_args()
root=pathlib.Path(tempfile.mkdtemp(prefix='crew-provider-live-',dir='/private/tmp'))
repo,home=root/'repo',root/'data'; repo.mkdir();home.mkdir();(repo/'.crew').mkdir()
(repo/'fixture.txt').write_text('The fixture value is 314159.\n')
skill=repo/'.crew'/'skills'/'acceptance-marker';skill.mkdir(parents=True)
(skill/'SKILL.md').write_text('For this isolated acceptance fixture, include the exact marker CREW_SHARED_SKILL_OK in your answer. Do not edit files.\n')
config='''default_agent="external"
[providers.external]
kind="cli"
command=["claude","--print","--verbose","--output-format","stream-json","--include-partial-messages"]
format="claude"
auth="subscription"
read_only_args=["--permission-mode","default","--disallowedTools","Edit,Write,NotebookEdit,Bash"]
resume_args=["--resume","{session}"]
capabilities=["read","tools","resume","stream"]
'''
(repo/'.crew'/'config.toml').write_text(config)
subprocess.run(['git','init','-q',str(repo)],check=True)
crew=str(pathlib.Path(options.crew).resolve()); env=dict(os.environ,CREW_HOME=str(home))
sid=''
def call(agent,task,name):
 global sid
 args=[crew,'run','--repo',str(repo),'--home',str(home),'--agent',agent]
 if sid:args+=['--session',sid]
 p=subprocess.run(args+[task],env=env,capture_output=True,text=True,timeout=240)
 (root/(name+'.stdout')).write_text(p.stdout);(root/(name+'.stderr')).write_text(p.stderr)
 if p.returncode:raise RuntimeError(name+': '+p.stderr[-1500:])
 if not sid:sid=re.search(r'Session ([a-f0-9]+)',p.stderr).group(1)
 assert 'CREW_SHARED_SKILL_OK' in p.stdout, 'custom skill not observed: '+name
 assert '314159' in p.stdout, 'fixture read not observed: '+name
 print(name+' passed',flush=True)
print('ROOT='+str(root),flush=True)
call('external','Read fixture.txt using read tools, report its exact numeric value and the custom skill marker. Do not edit files or run shell. End with PLUGIN_FIRST_OK.','external-first')
db=sqlite3.connect(home/'sessions.db')
native=db.execute('select id from native where session=? and agent=?',(sid,'external')).fetchone()[0]
call('codex','Read fixture.txt, report its numeric value and the custom skill marker. Check the shared journal for PLUGIN_FIRST_OK and confirm it is present. Do not modify files.','codex-shared')
call('external','Report the numeric fixture value and custom skill marker again. Use the shared journal to confirm the Codex turn is visible. Do not edit files or run shell. End with PLUGIN_RESUME_OK.','external-resume')
after=db.execute('select id from native where session=? and agent=?',(sid,'external')).fetchone()[0]
assert native==after, 'plugin did not resume same native conversation'
events=db.execute('select kind,agent,content from events where session=? order by seq',(sid,)).fetchall()
assert any(x[0]=='routing' and x[1]=='external' for x in events)
report={'state':'passed','root':str(root),'session':sid,'external_plugin':'configured cli, Claude subscription login','native_resume_same_id':native==after,'providers':['external','codex'],'custom_skill_observed':True,'turns':3}
(root/'result.json').write_text(json.dumps(report,indent=2));print(json.dumps(report),flush=True)
