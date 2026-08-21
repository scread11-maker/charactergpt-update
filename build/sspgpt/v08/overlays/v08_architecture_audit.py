#!/usr/bin/env python3
from pathlib import Path
import json, sys
ROOT=Path(__file__).resolve().parents[1]
errors=[]
def need(path, text=None):
    p=ROOT/path
    if not p.exists(): errors.append(f'missing:{path}'); return ''
    s=p.read_text(encoding='utf-8',errors='replace') if p.is_file() else ''
    if text and text not in s: errors.append(f'{path}:missing:{text}')
    return s

go=need('go.mod','module sspgpt/v08')
if 'go 1.27.0' not in go: errors.append('go.mod:not-go-1.27.0')
if any(line.strip().startswith('toolchain ') for line in go.splitlines()): errors.append('go.mod:unexpected-redundant-toolchain-directive')
if need('.go-version').strip()!='1.27.0': errors.append('.go-version:not-1.27.0')
for p in ['cmd/runtime/main.go','cmd/bridge/main.go','cmd/memory/main.go','cmd/behavior/main.go','cmd/touch/main.go','internal/abi/types.go','internal/store/memory.go','tools/verify_windows_binary.py','package/ghost/master/sspgpt_host.dic','package/ghost/master/sspgpt_touch.dic','package/ghost/master/sspgpt_presentation.dic']:
    need(p)
yaya=need('package/ghost/master/sspgpt_touch.dic')
for forbidden in ['heavy_tap','gentle_stroke','rough_rub','resting_touch','grab']:
    if forbidden in yaya: errors.append(f'yaya-classifies:{forbidden}')
runtime=need('cmd/runtime/main.go')
for forbidden in ['gentle_stroke','rough_rub','heavy_tap','resting_touch']:
    if forbidden in runtime: errors.append(f'runtime-classifies:{forbidden}')
abi=need('internal/abi/types.go')
for required in ['NativeGhostEvent','HostEventRouteDecision','HostStateSnapshot','HostStateDelta','TouchSourceEvent','NormalizedTouchObservation','CognitionFrame','BridgePromptProjection','SemanticReaction','SemanticPresentationIntent','HostActionObservation','CapabilityRequest','CapabilityResult']:
    if required not in abi: errors.append(f'abi-missing:{required}')
for p in (ROOT/'package').rglob('*'):
    parts={x.lower() for x in p.parts}
    if 'profile' in parts: errors.append(f'package-has-profile:{p.relative_to(ROOT)}')
    if 'plug' in parts: errors.append(f'package-has-plug:{p.relative_to(ROOT)}')
for required in ['SSPGPTMemoryService.exe','SSPGPTBehaviorService.exe','SSPGPTBridge.exe','SSPGPTTouchProgress.exe']:
    if required not in runtime: errors.append(f'runtime-child-missing:{required}')
for forbidden in ['ContextService.exe','MCPAdapter.exe','SSPGPTProviderService.exe']:
    if forbidden in runtime: errors.append(f'first-alchemy-forbidden-process:{forbidden}')
for p in ['package/ghost/master/config/behavior/behavior.json','package/ghost/master/config/memory/memory.json','package/ghost/master/config/provider/provider.json','package/ghost/master/config/runtime/runtime.json','package/ghost/master/config/touch/touch.json','package/ghost/master/config/input_ui.json','package/ghost/master/config/host_capabilities.json']:
    try: json.loads(need(p))
    except Exception as e: errors.append(f'invalid-json:{p}:{e}')
if errors:
    print('V08_ARCHITECTURE_AUDIT=FAIL')
    for e in errors: print(' -',e)
    sys.exit(1)
print('V08_ARCHITECTURE_AUDIT=PASS')
