#!/usr/bin/env python3
"""Builds a self-contained mocks page from the Fyr design system files."""
import os
D = os.path.join(os.path.dirname(__file__), "..", "system")
rd = lambda p: open(os.path.join(D, p)).read()
tokens_css = rd("tokens.css")
bundle_css = rd("components/bundle.css")
bundle_js = rd("components/bundle.js")

mock_css = r"""
html, body { margin: 0; background: transparent; }
.stage { display: flex; flex-direction: column; gap: 20px; font-family: var(--font-sans); }
.cap { font: 600 12px/16px var(--font-sans); letter-spacing: .04em; text-transform: uppercase; color: var(--foreground, #888); opacity: .7; margin: 4px 0 -12px; }
.frame { border-radius: 12px; overflow: hidden; border: 1px solid rgba(127,127,127,.25); }
.desk { zoom: 0.6; width: 1200px; height: 780px; display: grid; grid-template-columns: 400px minmax(0,1fr); background: var(--surface); color: var(--ink); }
.inbox { border-right: 1px solid var(--line); display: flex; flex-direction: column; min-width: 0; background: var(--surface-raised); }
.ibar { display: flex; align-items: center; gap: 8px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.brand { font: 600 15px/20px var(--font-sans); letter-spacing: -.01em; }
.lens { margin-left: auto; display: flex; gap: 4px; }
.chip { font: 500 12px/16px var(--font-sans); padding: 3px 8px; border-radius: 3px; border: 1px solid var(--line); color: var(--ink-muted); background: var(--surface-raised); }
.chip.on { border-color: var(--line-strong); color: var(--ink); background: var(--surface-sunken); }
.search { margin: 10px 16px; display: flex; align-items: center; gap: 8px; padding: 6px 10px; border: 1px solid var(--line); border-radius: 6px; color: var(--ink-faint); font: 400 13px/18px var(--font-sans); background: var(--surface); }
.search .fy-kbd { margin-left: auto; }
.thread { display: flex; flex-direction: column; min-width: 0; background: var(--surface); }
.thead { display: flex; align-items: center; gap: 10px; padding: 14px 24px; border-bottom: 1px solid var(--line); background: var(--surface-raised); flex-wrap: wrap; }
.thead .title { font: 600 20px/28px var(--font-sans); letter-spacing: -.01em; margin-right: 8px; }
.thead .right { margin-left: auto; display: flex; gap: 8px; align-items: center; }
.prof { font: 400 11px/16px var(--font-mono); color: var(--ink-muted); border: 1px solid var(--line); border-radius: 3px; padding: 1px 6px; }
.tbody { flex: 1; overflow: hidden; padding: 20px 24px; display: flex; flex-direction: column; gap: 16px; }
.msg { max-width: 720px; font: 400 15px/24px var(--font-sans); }
.msg.user { align-self: flex-end; background: var(--surface-sunken); border-radius: 10px; padding: 10px 14px; }
.who { font: 600 12px/16px var(--font-sans); color: var(--ink-muted); margin-bottom: 2px; }
.run { max-width: 720px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-raised); }
.run-h { display: flex; gap: 8px; align-items: center; padding: 8px 12px; border-bottom: 1px solid var(--line); font: 400 12px/16px var(--font-sans); color: var(--ink-muted); }
.tool { display: grid; grid-template-columns: 16px minmax(0,1fr) auto; gap: 10px; padding: 7px 12px; font: 400 12px/18px var(--font-mono); color: var(--ink); border-bottom: 1px solid var(--line); }
.tool:last-child { border-bottom: 0; }
.tool .ok { color: var(--calm); } .tool .t { color: var(--ink-faint); }
.tool .sub { font-family: var(--font-sans); color: var(--ink-muted); }
.composer { margin: 0 24px 20px; border: 1px solid var(--line-strong); border-radius: 10px; background: var(--surface-raised); }
.composer .in { padding: 12px 14px; font: 400 15px/24px var(--font-sans); color: var(--ink-faint); min-height: 24px; }
.composer .bar { display: flex; align-items: center; gap: 8px; padding: 6px 8px 6px 14px; border-top: 1px solid var(--line); font: 400 12px/16px var(--font-sans); color: var(--ink-muted); }
.composer .bar .sp { margin-left: auto; }
.seg { display: inline-flex; border: 1px solid var(--line); border-radius: 6px; overflow: hidden; }
.seg span { padding: 2px 8px; font: 500 12px/18px var(--font-sans); color: var(--ink-muted); }
.seg span.on { background: var(--surface-sunken); color: var(--ink); }
.phones { display: flex; gap: 16px; flex-wrap: wrap; }
.phone { width: 360px; height: 720px; zoom: 0.8; border-radius: 28px; overflow: hidden; display: flex; flex-direction: column; background: var(--surface); color: var(--ink); border: 1px solid rgba(127,127,127,.3); }
.pbar { display: flex; align-items: center; gap: 8px; padding: 14px 16px 10px; background: var(--surface-raised); border-bottom: 1px solid var(--line); }
.pfoot { margin-top: auto; display: flex; justify-content: space-around; padding: 10px 0 18px; border-top: 1px solid var(--line); background: var(--surface-raised); font: 500 11px/14px var(--font-sans); color: var(--ink-muted); }
.pfoot b { color: var(--ink); font-weight: 600; }
.fab { position: relative; }
.approve { margin: 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-raised); overflow: hidden; }
.approve .h { padding: 10px 14px; background: var(--beam-wash); border-bottom: 1px solid var(--line); font: 400 12px/16px var(--font-sans); color: var(--ink-muted); }
.approve .h strong { color: var(--beam-ink); }
.approve .b { padding: 14px; display: flex; flex-direction: column; gap: 10px; }
.cmd { font: 400 13px/20px var(--font-mono); background: var(--surface-sunken); border: 1px solid var(--line); border-radius: 6px; padding: 10px 12px; color: var(--ink); word-break: break-all; }
.kv { display: grid; grid-template-columns: 90px minmax(0,1fr); gap: 4px 10px; font: 400 12px/16px var(--font-sans); color: var(--ink-muted); }
.kv b { font-weight: 500; color: var(--ink); font-family: var(--font-mono); font-size: 11px; }
.big { height: 44px; justify-content: center; font-size: 15px; }
.coord { zoom: 0.6; width: 1200px; height: 640px; display: grid; grid-template-columns: minmax(0,1fr) 380px; background: var(--surface); color: var(--ink); }
.cmain { padding: 20px 24px; display: flex; flex-direction: column; gap: 18px; overflow: hidden; }
.cside { border-left: 1px solid var(--line); background: var(--surface-raised); padding: 20px; display: flex; flex-direction: column; gap: 18px; }
.h2 { font: 600 12px/16px var(--font-sans); letter-spacing: .04em; text-transform: uppercase; color: var(--ink-muted); }
.step { display: grid; grid-template-columns: 16px minmax(0,1fr) auto; gap: 10px; align-items: start; font: 400 15px/22px var(--font-sans); padding: 4px 0; }
.step .s { font: 400 12px/22px var(--font-sans); color: var(--ink-muted); }
.prop { border: 1px solid var(--line); border-radius: 10px; background: var(--surface-raised); }
.prop .r { display: grid; grid-template-columns: 18px minmax(0,1fr); gap: 10px; padding: 8px 12px; border-bottom: 1px solid var(--line); font: 400 13px/18px var(--font-sans); }
.prop .r code { font: 400 12px/18px var(--font-mono); color: var(--ink-muted); }
.box { width: 14px; height: 14px; border: 1.5px solid var(--line-strong); border-radius: 3px; margin-top: 2px; }
.box.on { background: var(--night); border-color: var(--night); }
.log { font: 400 12px/20px var(--font-mono); color: var(--ink-muted); }
.log b { color: var(--ink); font-weight: 500; }
.meter { height: 6px; border-radius: 3px; background: var(--surface-sunken); overflow: hidden; }
.meter i { display: block; height: 100%; width: 38%; background: var(--ink-muted); }
.grant { font: 400 12px/18px var(--font-sans); color: var(--ink-muted); }
.grant b { color: var(--ink); font-weight: 500; }
.toastwrap { position: absolute; left: 50%; bottom: 96px; transform: translateX(-50%); }
"""

app = r"""
var F = window.Fyr, h = React.createElement, R = ReactDOM;
function Inbox() {
  return h('div', {className:'inbox'},
    h('div', {className:'ibar'}, h('span',{className:'brand'},'Fyr'), h(F.HostChip,{name:'desk'}), h(F.HostChip,{name:'laptop'}), h(F.HostChip,{name:'build-01',offline:true}),
      h('div',{className:'lens'}, h('span',{className:'chip on'},'All'), h('span',{className:'chip'},'In hours'))),
    h('div',{className:'search'}, 'Go to thread or action', h(F.Kbd,null,'⌘K')),
    h(F.SectionHeader,{title:'Needs you',count:4,kind:'attention'}),
    h(F.ThreadRow,{state:'asking',title:'Fix map lasso on mobile',reason:'2 questions',summary:'Which gesture closes the sheet?',pr:{number:4182,state:'open'},time:'2 min',selected:true}),
    h(F.ThreadRow,{state:'failed',title:'Speed up e2e suite',reason:'API error',summary:'stopped after 14 min · retry',time:'9 min'}),
    h(F.ThreadRow,{state:'blocked',title:'Listing cards v2',reason:'CI failed',summary:'lint: 2 errors in CardGrid.tsx',pr:{number:4177,state:'failed'},time:'21 min'}),
    h(F.ThreadRow,{state:'done',title:'Triage sprint issues',summary:'Filed 6 issues, closed 2 duplicates',time:'1 h'}),
    h(F.SectionHeader,{title:'Working',count:6,open:false,hint:'1 waiting for build slot'}),
    h(F.SectionHeader,{title:'Seen',count:5,open:false}),
    h(F.SectionHeader,{title:'Snoozed',count:3,open:false,hint:'next: tomorrow 06:00'}),
    h(F.SectionHeader,{title:'Settled',count:11,open:false}));
}
function Run() {
  return h('div',{className:'run'},
    h('div',{className:'run-h'}, h(F.StatusBadge,{state:'asking',label:'Waiting for you'}), h('span',null,'· run 7 · 3 min · 41k tokens')),
    h('div',{className:'tool'}, h('span',{className:'ok'},'✓'), h('span',null,'pnpm vitest run src/map/lasso'), h('span',{className:'t'},'38 s')),
    h('div',{className:'tool'}, h('span',{className:'ok'},'✓'), h('span',{className:'sub'},'3 subagents · explored gesture handling in 14 files'), h('span',{className:'t'},'show')),
    h('div',{className:'tool'}, h('span',{className:'ok'},'✓'), h('span',null,'edit src/map/FilterSheet.tsx  +42 −18'), h('span',{className:'t'},'diff')));
}
function Thread() {
  return h('div',{className:'thread'},
    h('div',{className:'thead'}, h('span',{className:'title'},'Fix map lasso on mobile'), h(F.StatusBadge,{state:'asking'}), h(F.PRBadge,{number:4182,state:'open'}),
      h('div',{className:'right'}, h('span',{className:'prof'},'sonnet 5.5 · main account'), h(F.HostChip,{name:'desk'}), h(F.Button,{variant:'ghost',kbd:'S'},'Snooze'), h(F.Button,{kbd:'E',disabled:true,reason:'A question is open'},'Settle'))),
    h('div',{className:'tbody'},
      h(F.DecisionCard,{from:'this thread',unblocks:'this run',price:'20 s',defaultText:'No default · parks in 10 min to free memory',questions:[
        {text:'Which gesture should close the filter sheet?',options:[{label:'Swipe down',desc:'Matches the iOS sheet the app already uses.'},{label:'Tap outside',desc:'Simpler; conflicts with the lasso on small screens.'},{label:'Both',desc:'Most forgiving; two code paths to test.'}]},
        {text:'Which screens get the new sheet?',multi:true,options:[{label:'Map'},{label:'List'},{label:'Saved searches'}]}]}),
      h(Run)),
    h('div',{className:'composer'}, h('div',{className:'in'},'Reply, or drop a screenshot…'),
      h('div',{className:'bar'}, h('span',{className:'seg'}, h('span',{className:'on'},'Steer'), h('span',null,'Queue')), h('span',null,'⌘⏎ sends in background'), h('span',{className:'sp'}), h(F.Button,{variant:'primary',kbd:'⏎'},'Send'))));
}
function Desk() { return h('div',{className:'desk'}, h(Inbox), h(Thread)); }

function PhoneInbox() {
  return h('div',{className:'phone'},
    h('div',{className:'pbar'}, h('span',{className:'brand'},'Fyr'), h('span',{style:{marginLeft:'auto'}}, h(F.Button,{variant:'primary'},'+ New'))),
    h(F.SectionHeader,{title:'Needs you',count:3,kind:'attention'}),
    h(F.ThreadRow,{state:'asking',title:'Merge #4170?',reason:'approval · Face ID',summary:'Babysit PR #4170',time:'now'}),
    h(F.ThreadRow,{state:'asking',title:'Fix map lasso on mobile',reason:'2 questions',time:'2 min'}),
    h(F.ThreadRow,{state:'blocked',title:'Listing cards v2',reason:'checks failed',pr:{number:4177,state:'failed'},time:'21 min'}),
    h(F.SectionHeader,{title:'Working',count:6,open:false}),
    h(F.SectionHeader,{title:'Snoozed',count:3,open:false}),
    h('div',{className:'pfoot'}, h('b',null,'Inbox'), h('span',null,'Search'), h('span',null,'Hosts')));
}
function PhoneApprove() {
  return h('div',{className:'phone'},
    h('div',{className:'pbar'}, h('span',{className:'brand'},'‹ Inbox')),
    h('div',{className:'approve'},
      h('div',{className:'h'}, h('strong',null,'Approval'), ' · Babysit PR #4170 · unblocks 1 run'),
      h('div',{className:'b'},
        h('div',{className:'fy-q-text'},'Merge pull request #4170?'),
        h('div',{className:'cmd'},'gh pr merge 4170 --squash --delete-branch'),
        h('div',{className:'kv'}, h('span',null,'host'), h('b',null,'desk'), h('span',null,'directory'), h('b',null,'~/acme/web'), h('span',null,'checks'), h('b',null,'14/14 passed'), h('span',null,'policy'), h('b',null,'merge → verified (v3)'), h('span',null,'expires'), h('b',null,'in 23 h · then denied')),
        h(F.Button,{variant:'primary',className:'big'},'Approve with Face ID'),
        h(F.Button,{className:'big'},'Deny'))),
    h('div',{className:'pfoot'}, h('b',null,'Inbox'), h('span',null,'Search'), h('span',null,'Hosts')));
}
function Coord() {
  var step = function(s, text, st){ return h('div',{className:'step'}, h('span',{className:'fy-s-'+s}, h(F.StatusBadge,{state:s,label:' '})), h('span',null,text), h('span',{className:'s'},st)); };
  return h('div',{className:'coord'},
    h('div',{className:'cmain'},
      h('div',{style:{display:'flex',alignItems:'center',gap:'10px'}}, h('span',{className:'title',style:{font:'600 20px/28px var(--font-sans)'}},'PR babysitter · acme'), h(F.StatusBadge,{state:'working',label:'Running'}), h('span',{style:{marginLeft:'auto',display:'flex',gap:'8px'}}, h(F.Button,{variant:'ghost'},'Wake'), h(F.Button,{variant:'danger'},'Stop'))),
      h('div',{className:'h2'},'Plan'),
      h('div',null, step('settled','Rebase #4170 on main','done 09:12'), step('working','Fix lint in #4177 (steering Listing cards v2)','running'), step('waiting','#4165 waits for review from a person','since 08:40'), step('seen','Nightly: check stale PRs older than 3 days','06:00')),
      h('div',{className:'h2'},'Proposal · needs you'),
      h('div',{className:'prop'},
        h('div',{className:'r'}, h('span',{className:'box on'}), h('span',null,'Start a thread for flaky test ', h('code',null,'e2e/map.spec.ts'))),
        h('div',{className:'r'}, h('span',{className:'box on'}), h('span',null,'Snooze ', h('code',null,'#4165'), ' until a review arrives')),
        h('div',{className:'r'}, h('span',{className:'box'}), h('span',null,'Close stale PR ', h('code',null,'#4012'), ' (outside grant)')),
        h('div',{style:{display:'flex',gap:'8px',padding:'10px 12px'}}, h(F.Button,{variant:'primary'},'Accept 2'), h(F.Button,{variant:'ghost'},'Reject')))),
    h('div',{className:'cside'},
      h('div',{className:'h2'},'Activity'),
      h('div',{className:'log'}, h('div',null,'09:31 ',h('b',null,'thread.send'),' Listing cards v2'), h('div',null,'09:12 ',h('b',null,'thread.start'),' rebase #4170'), h('div',null,'08:40 ',h('b',null,'thread.snooze'),' #4165'), h('div',null,'08:02 ',h('b',null,'digest'),' 4 changes')),
      h('div',{className:'h2'},'Budget today'),
      h('div',null, h('div',{className:'meter'}, h('i')), h('div',{className:'grant',style:{marginTop:'6px'}},'380k of 1M tokens · 2 of 3 threads')),
      h('div',{className:'h2'},'Grant'),
      h('div',{className:'grant'}, h('div',null,'Repos ', h('b',null,'acme/web, acme/api')), h('div',null,'Actions ', h('b',null,'start, send, snooze, link')), h('div',null,'Mode ceiling ', h('b',null,'auto')), h('div',null,'Never ', h('b',null,'approve, merge, widen scope')), h('div',null,'Signed ', h('b',null,'Face ID · expires 13 Dec')))));
}
function Stage() {
  return h('div',{className:'stage'},
    h('div',{className:'cap'},'1 · Desktop · inbox and thread (light)'),
    h('div',{className:'frame','data-theme':'light'}, h(Desk)),
    h('div',{className:'cap'},'2 · Phone · inbox and a verified approval from a push (dark)'),
    h('div',{className:'phones'}, h('div',{'data-theme':'dark'}, h(PhoneInbox)), h('div',{'data-theme':'dark'}, h(PhoneApprove))),
    h('div',{className:'cap'},'3 · Coordinator pane (dark)'),
    h('div',{className:'frame','data-theme':'dark'}, h(Coord)));
}
R.createRoot(document.getElementById('root')).render(h(Stage));
"""

html = f"""<!doctype html>
<html><head><meta charset="utf-8"><title>Fyr screens</title>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=IBM+Plex+Sans:wght@400;500;600&display=swap">
<style>{tokens_css}
{bundle_css.split(chr(10),1)[1]}
{mock_css}
body {{ background: transparent; }}
</style>
<script src="https://cdnjs.cloudflare.com/ajax/libs/react/18.3.1/umd/react.production.min.js"></script>
<script src="https://cdnjs.cloudflare.com/ajax/libs/react-dom/18.3.1/umd/react-dom.production.min.js"></script>
</head><body><div id="root"></div>
<script>{bundle_js}</script>
<script>{app}</script>
</body></html>"""
out = os.path.join(os.path.dirname(__file__), "..", "screens.html")
open(out, "w").write(html)
print(out, len(html))
