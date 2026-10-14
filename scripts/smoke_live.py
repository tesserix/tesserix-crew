#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Opt-in live Claude/Codex acceptance; uses model calls and an isolated fixture."""
import argparse, json, os, pathlib, signal, subprocess, sys, tempfile, time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--crew', required=True, help='Crew executable to verify')
options = parser.parse_args()

root = pathlib.Path(tempfile.mkdtemp(prefix='crew-live-acceptance-'))
repo, home = root / 'repo', root / 'data'
repo.mkdir(); home.mkdir(); (repo / '.crew').mkdir()
(repo / 'arithmetic.py').write_text('def add(a, b):\n    return a - b\n')
(repo / 'test_unit.py').write_text('import unittest\nfrom arithmetic import add\nclass AdditionTests(unittest.TestCase):\n    def test_add(self):\n        self.assertEqual(add(2, 3), 5)\n')
(repo / 'test_acceptance.py').write_text('import unittest\nfrom arithmetic import add\nclass AcceptanceTests(unittest.TestCase):\n    def test_negative(self):\n        self.assertEqual(add(-2, -3), -5)\n    def test_zero(self):\n        self.assertEqual(add(0, 7), 7)\n    def test_decimal(self):\n        self.assertAlmostEqual(add(1.25, 2.5), 3.75)\n')
python = sys.executable
config = '''[lifecycles.live-acceptance]
description="Isolated live-provider coding and recovery acceptance"
tdd=true
max_repairs=1
[[lifecycles.live-acceptance.checks]]
name="unit"
kind="unit"
command=PYTHON_UNIT
[[lifecycles.live-acceptance.checks]]
name="acceptance"
kind="integration"
command=PYTHON_ACCEPTANCE
roles=["tester"]
'''.replace('PYTHON_UNIT', json.dumps([python, '-m', 'unittest', 'test_unit.py'])).replace('PYTHON_ACCEPTANCE', json.dumps([python, '-m', 'unittest', 'test_acceptance.py']))
stages = [
 ('design','designer','claude',False,'Inspect arithmetic.py and the tests. Define the acceptance criteria for correcting addition for positive, negative, zero and decimal inputs. Do not edit files. Keep the response under 200 words.'),
 ('plan','planner','claude',False,'Produce a concise TDD implementation plan for addition with meaningful red/green unit checks and independent acceptance checks. Do not edit files. No GitHub issue proposals needed for this isolated fixture.'),
 ('review','reviewer','codex',False,'Independently review the design and plan against the fixture tests. Do not edit files. Keep the response under 200 words.'),
 ('implement','implementer','codex',True,'Fix arithmetic.py using TDD. Add a meaningful edge-case unit regression if necessary. Use the Crew run_check MCP tool to record unit red before production edits, then unit green. On a repair attempt, inspect tester feedback, record fresh red/green evidence and fix the reproduced regression. Change only this fixture repository. Never modify .crew/config.toml or the pinned acceptance tests.'),
 ('test','tester','claude',True,'Independently inspect arithmetic.py and test the acceptance criteria. Run and record both unit and acceptance checks with Crew run_check. Do not modify production code or .crew/config.toml. Give a concrete reproduction for failures. Keep the response under 250 words.'),
 ('deliver','deliverer','claude',False,'Summarize implementation and independent testing evidence, including the failed test and repair if present. Do not edit files, create issues or publish. Keep the response under 250 words.'),
]
for name, role, agent, edits, prompt in stages:
    if name == 'implement':
        config += '\n[[lifecycles.live-acceptance.stages]]\nname="user-review"\nkind="approval"\nprompt="Approve only the isolated arithmetic fixture and its named executable checks"\n'
    config += '\n[[lifecycles.live-acceptance.stages]]\n' + '\n'.join([f'name={json.dumps(name)}','kind="agent"',f'role={json.dumps(role)}',f'agent={json.dumps(agent)}',f'allow_edits={str(edits).lower()}','timeout="4m"',f'prompt={json.dumps(prompt)}'])+'\n'
(repo / '.crew' / 'config.toml').write_text(config)
subprocess.run(['git','init','-q',str(repo)],check=True)
crew = str(pathlib.Path(options.crew).resolve())
env = dict(os.environ, CREW_HOME=str(home))
run_id = ''
timeline = []
def call(action, *args, expected=None):
    cmd=[crew,'workflow',action,'--repo',str(repo),'--home',str(home),'--json',*args]
    proc=subprocess.run(cmd,env=env,capture_output=True,text=True,timeout=270)
    (root/f'{len(timeline):02d}-{action}.stdout').write_text(proc.stdout)
    (root/f'{len(timeline):02d}-{action}.stderr').write_text(proc.stderr)
    try: result=json.loads(proc.stdout)
    except Exception: raise RuntimeError(f'{action} returned no workflow JSON: {proc.stderr[-1500:]}')
    state=result.get('state') if isinstance(result,dict) else None
    timeline.append({'action':action,'exit':proc.returncode,'state':state,'stage':result.get('current') if isinstance(result,dict) else None})
    (root/'timeline.json').write_text(json.dumps(timeline,indent=2))
    print(json.dumps(timeline[-1]),flush=True)
    if expected is not None and state not in expected: raise RuntimeError(f'Unexpected state {state} from {action}; inspect {root}')
    return result

print('ROOT='+str(root),flush=True)
w=call('start','--preset','live-acceptance','Correct addition and independently verify signed, zero and decimal inputs',expected=['ready'])
run_id=w['id']
(root/'state.json').write_text(json.dumps({'root':str(root),'repo':str(repo),'home':str(home),'id':run_id}))
log=root/'interrupted.stdout'; err=root/'interrupted.stderr'
with log.open('w') as out, err.open('w') as stderr:
    process=subprocess.Popen([crew,'workflow','next','--repo',str(repo),'--home',str(home),'--json',run_id],env=env,stdout=out,stderr=stderr)
    time.sleep(4)
    if process.poll() is None: process.send_signal(signal.SIGINT)
    process.wait(timeout=30)
w=call('status',run_id)
if w['state'] in ['failed','blocked']:
    w=call('retry','--note','Interrupted fixture process has stopped; no production edits occurred. Resume design.',run_id,expected=['ready'])
elif w['current'] == 0: raise RuntimeError('Cancellation left an unrecovered running stage')
while w['state']=='ready' and w['current']<3:
    w=call('next',run_id,expected=['ready','awaiting-approval'])
if w['state']!='awaiting-approval': raise RuntimeError('Live planning failed before approval')
before=(repo/'arithmetic.py').read_text()
blocked=call('next','--allow-edits',run_id,expected=['awaiting-approval'])
assert (repo/'arithmetic.py').read_text()==before
# The user authorized this concrete isolated acceptance run and named checks.
w=call('approve','--note','Authorized isolated fixture scope: arithmetic addition, unit/integration checks, no production or GitHub writes.',run_id,expected=['ready'])
w=call('next','--allow-edits',run_id,expected=['ready'])
# Controlled residual regression for independent discovery and bounded repair.
source=(repo/'arithmetic.py').read_text()
(root/'implemented-arithmetic.py').write_text(source)
(repo/'arithmetic.py').write_text('def add(a, b):\n    if a < 0 and b < 0:\n        return a - b\n    return a + b\n')
w=call('next','--allow-edits',run_id,expected=['failed','blocked'])
assert w['state']=='failed', 'Tester must demonstrate the seeded regression, not unavailable tooling'
w=call('repair','--note','Independent acceptance found negative+negative subtraction. Add a regression unit test, record fresh red/green evidence and repair addition.',run_id,expected=['ready'])
w=call('next','--allow-edits',run_id,expected=['ready'])
w=call('next','--allow-edits',run_id,expected=['ready'])
w=call('next',run_id,expected=['completed'])
checks=call('evidence',run_id)
assert any(not x['passed'] and x['check']=='acceptance' for x in checks)
assert any(x['passed'] and x['check']=='acceptance' for x in checks)
assert {x['agent'] for x in w['results'] if x.get('stage')=='implement' and x['state'] in ['pass','fail','blocked']} == {'codex'}
assert {x['agent'] for x in w['results'] if x.get('stage')=='test' and x['state'] in ['pass','fail','blocked']} == {'claude'}
(root/'result.json').write_text(json.dumps({'state':w['state'],'root':str(root),'id':run_id,'checks':len(checks),'limitations':['No GitHub create/close stages','CLI fixture, no browser task','Controlled seeded regression'], 'workflow':w},indent=2))
print('ACCEPTANCE_PASSED '+str(root),flush=True)
