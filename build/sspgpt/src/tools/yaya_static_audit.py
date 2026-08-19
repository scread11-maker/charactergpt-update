from pathlib import Path
import re, json, hashlib
root=Path(__file__).resolve().parents[1]/'yaya_adapter'/'ghost'/'master'
files=[root/'cgpt_core.dic',root/'cgpt_touch.dic']
report={}
funcs=set()
for p in files:
    text=p.read_text('utf-8')
    lines=text.splitlines()
    local_funcs=set()
    for i,l in enumerate(lines[:-1]):
        n=l.strip()
        if re.fullmatch(r'[A-Za-z_][A-Za-z0-9_.]*',n) and lines[i+1].strip()=='{':
            funcs.add(n); local_funcs.add(n)
    # Lightweight lexical balance ignoring // comments and strings.
    brace=0; in_s=None; esc=False
    for ln,line in enumerate(lines,1):
        i=0
        while i<len(line):
            c=line[i]
            if in_s:
                # YAYA SakuraScript convention uses literal single backslashes;
                # only quote closure matters for this lightweight audit.
                if c==in_s: in_s=None
                i+=1; continue
            if c in "'\"": in_s=c; i+=1; continue
            if c=='/' and i+1<len(line) and line[i+1]=='/': break
            if c=='{': brace+=1
            elif c=='}': brace-=1
            if brace<0: raise SystemExit(f'{p}:{ln}: negative brace balance')
            i+=1
        if in_s:
            raise SystemExit(f'{p}:{ln}: unclosed string')
    if brace!=0: raise SystemExit(f'{p}: brace balance {brace}')
    report[p.name]={
        'lines':len(lines),
        'functions':len(local_funcs),
        'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),
        'double_backslash_sequences':text.count('\\\\'),
    }

alltxt='\n'.join(p.read_text('utf-8') for p in files)
refs=set(re.findall(r'(?:timerraise|raise),[^\]"\n]*?,(?:1,)?([A-Za-z_][A-Za-z0-9_]*)',alltxt))
refs |= set(re.findall(r'\\q\[[^,\]]+,([A-Za-z_][A-Za-z0-9_]*)',alltxt))
missing=sorted(x for x in refs if x not in funcs)
report['function_count']=len(funcs)
report['script_event_refs']=sorted(refs)
report['missing_script_event_targets']=missing
report['unicode_probe_present']=all(x in alltxt for x in ['檢查並更新','說話','查詢','繁體中文','日本語'])
report['satori_artifacts']=sorted(str(p.relative_to(root)) for p in root.rglob('*') if p.is_file() and ('satori' in p.name.lower()))
report['legacy_fullwidth_satori_syntax']=any(x in alltxt for x in ['＊','＄','＠','＞'])
yaya_txt=(root/'yaya.txt').read_text('utf-8-sig')
report['utf8_transport_config']=all(x in yaya_txt for x in [
    'charset.dic, UTF-8','charset.output, UTF-8','charset.file, UTF-8',
    'charset.save, UTF-8','charset.extension, UTF-8'])

report['fix6_balloon_guard']={
    'status_uses_parameterized_balloon': "'balloon(' _in_ status" in alltxt,
    'logical_foreground_guard': 'CGPT.BalloonGuardActive = 1' in alltxt and 'if CGPT.BalloonGuardActive == 1' in alltxt,
    'guard_clears_on_close_edges': alltxt.count('CGPT.BalloonGuardActive = 0') >= 4,
    'preserve_helper_present': 'CGPT.PreserveBalloonScript' in alltxt,
    'automatic_apply_uses_control_head': '_head = CGPT.ControlHead()' in alltxt,
    'presentation_busy_explicit_calls': 'CGPT.IsPresentationBusy()' in alltxt,
}
if not all(report['fix6_balloon_guard'].values()):
    raise SystemExit('fix6 balloon-preservation guard incomplete')

report['fix7_connection_staging']={
    'runtime_connected_state': 'CGPT.RuntimeConnected = 0' in alltxt and 'CGPT.RuntimeConnected = 1' in alltxt,
    'boot_hides_main_shows_owl': r'\0\s[-1]\1\s[10]CharacterGPT v0.7.1' in alltxt,
    'open_reveals_both': r'\0\s[25]\1\s[10]\0' in alltxt,
    'failure_is_owl_scope': r'\0\s[-1]\1\s[10]\![set,balloontimeout,0]CharacterGPT Runtime 尚未連線' in alltxt,
    'affect_cannot_reveal_disconnected_main': 'if CGPT.RuntimeConnected != 1' in alltxt and r'\0\s[-1]\1\s[10]\e' in alltxt,
}
if not all(report['fix7_connection_staging'].values()):
    raise SystemExit('fix7 Owl connection-staging guard incomplete')
if missing: raise SystemExit('Missing script event targets: '+', '.join(missing))
if not report['unicode_probe_present']: raise SystemExit('Unicode probe missing')
if report['satori_artifacts']: raise SystemExit('Satori artifacts remain')
if report['legacy_fullwidth_satori_syntax']: raise SystemExit('Legacy Satori syntax remains')
if not report['utf8_transport_config']: raise SystemExit('UTF-8 YAYA transport config incomplete')
if any(report[p.name]['double_backslash_sequences'] for p in files):
    raise SystemExit('C-style double-backslash SakuraScript remains in YAYA adapter')
print(json.dumps(report,ensure_ascii=False,indent=2))
