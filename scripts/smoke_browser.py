#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Exercise TDD, independent browser failure, repair and durable screenshots.

Uses scripted provider adapters and real Playwright/Chromium, without model calls
or GitHub writes. Dependencies are supplied explicitly rather than bundled.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--crew', required=True)
    parser.add_argument('--playwright', required=True, help='Absolute Playwright package directory')
    parser.add_argument('--browser-executable', default='')
    options = parser.parse_args()
    root = Path(tempfile.mkdtemp(prefix='crew-browser-smoke-'))
    repo, home = root / 'repo', root / 'data'
    repo.mkdir()
    home.mkdir()
    (repo / '.crew').mkdir()
    (repo / 'math.mjs').write_text('export const add = (a, b) => a - b;\n')
    (repo / 'unit.mjs').write_text("import assert from 'node:assert/strict'; import {add} from './math.mjs'; assert.equal(add(2,3),5);\n")
    (repo / 'index.html').write_text('<input id="a"><input id="b"><button id="run">Add</button><output id="out"></output><script src="ui.js"></script>')
    (repo / 'ui.js').write_text("document.getElementById('run').onclick=()=>document.getElementById('out').textContent=Number(document.getElementById('a').value)-Number(document.getElementById('b').value);\n")
    (repo / 'browser.cjs').write_text('''const assert=require('node:assert/strict');
const path=require('node:path');
const {pathToFileURL}=require('node:url');
const {chromium}=require(process.env.CREW_SMOKE_PLAYWRIGHT);
(async()=>{
 const config={headless:true}; if(process.env.CREW_SMOKE_BROWSER) config.executablePath=process.env.CREW_SMOKE_BROWSER;
 const browser=await chromium.launch(config);
 try {
  const page=await browser.newPage(); await page.goto(pathToFileURL(path.join(process.cwd(),'index.html')).href);
  await page.fill('#a','2'); await page.fill('#b','3'); await page.click('#run');
  await page.screenshot({path:'shot.png'});
  assert.equal(await page.textContent('#out'),'5','adding 2 and 3 must show 5');
 } finally { await browser.close(); }
})().catch(error=>{console.error(error);process.exit(1)});
''')
    adapter = '''import json,os,pathlib,subprocess,sys
sys.stdin.read()
repo=pathlib.Path(os.environ['CREW_SMOKE_REPO']);home=pathlib.Path(os.environ['CREW_SMOKE_HOME']);sid=os.environ['CREW_SMOKE_ID'];crew=os.environ['CREW_SMOKE_CLI']
def check(name,phase):
 return subprocess.run([crew,'workflow','check','--home',str(home),'--repo',str(repo),'--name',name,'--phase',phase,'--json',sid],capture_output=True,text=True)
def result(text,state):
 print(json.dumps({'type':'text','text':text+'\\nCREW_STAGE_RESULT: '+state}))
'''
    (home / 'maker.py').write_text(adapter + '''countfile=home/'maker-count';count=int(countfile.read_text()) if countfile.exists() else 0;countfile.write_text(str(count+1))
if count>0:
 with (repo/'unit.mjs').open('a') as file:
  file.write("import vm from 'node:vm'; import fs from 'node:fs'; const nodes={a:{value:'2'},b:{value:'3'},out:{textContent:''},run:{}}; vm.runInNewContext(fs.readFileSync('ui.js','utf8'),{document:{getElementById:id=>nodes[id]}}); nodes.run.onclick(); assert.equal(nodes.out.textContent,5);\\n")
red=check('unit','red')
if red.returncode: result(red.stdout+red.stderr,'blocked');sys.exit(0)
if count==0: (repo/'math.mjs').write_text('export const add = (a, b) => a + b;\\n')
else: (repo/'ui.js').write_text((repo/'ui.js').read_text().replace(".value)-Number", ".value)+Number"))
green=check('unit','green');result('Recorded real red/green unit evidence' if green.returncode==0 else green.stdout+green.stderr,'pass' if green.returncode==0 else 'fail')
''')
    (home / 'checker.py').write_text(adapter + '''unit=check('unit','green');browser=check('browser','green')
if unit.returncode or browser.returncode: result('Independent browser reproduction: adding 2 and 3 did not show 5.\\n'+browser.stdout+browser.stderr,'fail')
else: result('Independent unit and real-browser checks passed; screenshot retained.','pass')
''')
    (home / 'reporter.py').write_text(adapter + "result('Delivery report: regression fixed and independently checked.','pass')\n")
    providers = []
    for name, script in [('maker', 'maker.py'), ('checker', 'checker.py'), ('reporter', 'reporter.py')]:
        providers.append(f'''[providers.{name}]
kind="cli"
command={json.dumps([sys.executable, str(home / script)])}
format="crew"
auth="none"
read_only_args=["--read"]
edit_args=["--write"]
capabilities=["read","write","tools"]
''')
    config = '\n'.join(providers) + '''
[lifecycles.browser-demo]
description="Seeded regression, independent browser discovery and bounded repair"
tdd=true
max_repairs=1
[[lifecycles.browser-demo.checks]]
name="unit"
kind="unit"
command=["node","unit.mjs"]
[[lifecycles.browser-demo.checks]]
name="browser"
kind="browser"
command=["node","browser.cjs"]
roles=["tester"]
artifacts=["shot.png"]
[[lifecycles.browser-demo.stages]]
name="user-review"
kind="approval"
prompt="Approve fixture scope and its executable checks"
[[lifecycles.browser-demo.stages]]
name="implement"
kind="agent"
role="implementer"
agent="maker"
allow_edits=true
prompt="Implement using red/green evidence"
[[lifecycles.browser-demo.stages]]
name="test"
kind="agent"
role="tester"
agent="checker"
allow_edits=true
prompt="Independently run the required unit/browser checks"
[[lifecycles.browser-demo.stages]]
name="deliver"
kind="agent"
role="deliverer"
agent="reporter"
prompt="Report verified delivery"
'''
    (repo / '.crew' / 'config.toml').write_text(config)
    env = dict(os.environ, CREW_SMOKE_REPO=str(repo), CREW_SMOKE_HOME=str(home),
               CREW_SMOKE_CLI=str(Path(options.crew).resolve()),
               CREW_SMOKE_PLAYWRIGHT=str(Path(options.playwright).resolve()),
               CREW_SMOKE_BROWSER=options.browser_executable)
    def call(action, *args, success=True):
        proc = subprocess.run([options.crew, 'workflow', action, '--repo', str(repo), '--home', str(home), '--json', *args], env=env, capture_output=True, text=True, timeout=120)
        if success and proc.returncode:
            raise RuntimeError(proc.stderr + proc.stdout)
        if not success and proc.returncode == 0:
            raise RuntimeError('Expected seeded regression to block testing')
        return json.loads(proc.stdout)
    run = call('start', '--preset', 'browser-demo', 'Fix addition regression')
    env['CREW_SMOKE_ID'] = run['id']
    run = call('approve', '--note', 'Fixture scope and named checks reviewed', run['id'])
    run = call('next', '--allow-edits', run['id'])
    run = call('next', '--allow-edits', run['id'], success=False)
    assert run['state'] == 'failed'
    failed_evidence = call('evidence', run['id'])
    failed_browser = [item for item in failed_evidence if item['check'] == 'browser'][-1]
    assert not failed_browser['passed'] and failed_browser['artifacts']
    failed_screenshot = failed_browser['artifacts'][0]['snapshot']
    run = call('repair', '--note', 'Browser found addition handler subtracts; add a regression unit test and fix the handler', run['id'])
    run = call('next', '--allow-edits', run['id'])
    run = call('next', '--allow-edits', run['id'])
    run = call('next', run['id'])
    assert run['state'] == 'completed'
    evidence = call('evidence', run['id'])
    green_browser = [item for item in evidence if item['check'] == 'browser' and item['passed']][-1]
    assert Path(failed_screenshot).is_file()
    assert Path(green_browser['artifacts'][0]['snapshot']).is_file()
    print(json.dumps({'state': run['state'], 'providers': ['maker', 'checker'], 'root': str(root), 'failing_screenshot': failed_screenshot, 'passing_screenshot': green_browser['artifacts'][0]['snapshot'], 'checks_recorded': len(evidence)}, indent=2))


if __name__ == '__main__':
    main()
