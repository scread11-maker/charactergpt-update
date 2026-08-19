#!/usr/bin/env python3
import hashlib,json,re,sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]

def text(p):
    try:return (ROOT/p).read_text(encoding='utf-8-sig')
    except:return ''
def sha(p):
    q=ROOT/p
    return hashlib.sha256(q.read_bytes()).hexdigest() if q.exists() else ''
def contains(p,s): return s in text(p)

def jload(p):
    try:return json.loads(text(p))
    except:return {}

checks={}
# fix14 build/toolchain baseline
go_mod=text('go.mod')
checks['go126_module_floor']=('go 1.26.2' in go_mod)
checks['go1266_preferred_toolchain']=('toolchain go1.26.6' in go_mod)
checks['go1266_version_file']=(text('.go-version').strip()=='1.26.6')
model=text('internal/model/model.go'); cfgmodel=text('internal/model/config.go')
rt=text('cmd/runtime/main.go'); linked=text('cmd/runtime/v071_linked.go')
br=text('cmd/bridge/main.go'); mem=text('cmd/memoryservice/main.go')
link=text('cmd/link/main.go'); core=text('yaya_adapter/ghost/master/cgpt_core.dic')
touch=text('cmd/touchprogress/main.go')

# canonical ownership / request classes
checks['request_linked_chat']=('RequestLinkedChat' in model and 'linked_chat' in model)
checks['request_appearance_change']=('RequestAppearance' in model and 'appearance_change' in model and 'AppearanceTransition' in model)
checks['episode_commit_v2']=('type EpisodeCommitV2 struct' in model)
checks['typed_memory_capsule']=('type MemoryCapsule struct' in model)
checks['recent_dialogue_fastpath']=('RecentDialogue' in model and 'RecentDialogue' in rt)
checks['recent_physical_for_rule_conditions']=('RecentPhysical' in model and 'RecentPhysical' in rt and 'rememberPhysicalOccurrence' in rt)

# Fast Path / memory split
checks['runtime_direct_bridge_fastpath']='http://127.0.0.1:8767/v1/respond' in rt
checks['runtime_selective_recall']='http://127.0.0.1:8768/v2/recall' in rt
checks['bridge_no_memory_consolidation']='memory_consolidation' not in br.lower()
checks['memory_local_brain']='ChatJSON' in mem and 'memory_evaluation_guide.md' in mem
checks['memory_invalid_output_quarantine']='quarantined_invalid_output' in mem and 'isTerminalMemoryBrainOutputError' in mem and 'quarantineInvalidOutput' in mem
checks['memory_transient_failure_still_deferred']='MEMORY_BRAIN_DEFER' in mem and 'time.After(60 * time.Second)' in mem
checks['raw_move_hard_filter']=('Event.Type == "move"' in mem or 'strings.EqualFold(ep.Event.Type, "move")' in mem)
checks['semantic_embedding']='storeSemantic' in mem and 'EmbeddingGeneration' in mem
checks['hybrid_retrieval']=all(x in mem for x in ['RRFK','CandidatePool','Rerank','rrfAdd'])
checks['hot_memory_push']='internal/memory/hot-v2' in mem
checks['retention_gate']='retentionScore' in mem and 'memory_retention_rules.json' in mem
checks['observation_evidence']='ObservationMinEvidence' in mem and 'evidenceCount' in mem
checks['contradiction_versioning']=('SupersededBy' in mem and 'ValidUntil' in mem)

# config single ownership
ret=jload('config/memory_retention_rules.json'); retr=jload('config/memory_retrieval_rules.json')
retrieval_keys={'semantic_recall_max_items','candidate_pool','context_budget_tokens','recall_timeout_ms','embedding_dimension','presets'}
checks['retention_has_no_retrieval_fields']=not any(k in ret for k in retrieval_keys)
checks['retrieval_is_candidate_authority']=retr.get('format_version')==3 and retr.get('embedding_dimension')==512 and retr.get('presets',{}).get('medium',{}).get('candidate_pool')==300 and retr.get('presets',{}).get('medium',{}).get('context_budget_tokens')==1024
checks['interaction_conditions_are_consumed']='MatchConditions' in cfgmodel and 'conditionsSatisfied' in br and 'RepeatCountGTE' in br and 'RecentChatWithinSeconds' in br
checks['reaction_style_recent_window_is_consumed']='recentDialogueForPrompt' in br and 'RecentContextSeconds' in br
linked_cfg=jload('config/linked_chat_rules.json')
checks['plug_opt_in_not_fake_config']='enabled_by_default' not in linked_cfg
checks['linked_completion_report_is_consumed']='LocalCompletionReport' in linked and 'OnCharacterGPTLinkedRelease' in linked and 'completion_report=%t' in linked

# local models
lm=jload('config/local_models.json')
checks['embedding_512_generation1']=lm.get('embedding_dimension')==512 and lm.get('embedding_generation')==1
checks['memory_model_pinned_sha']=len(lm.get('memory_llm',{}).get('sha256',''))==64
checks['embedder_pinned_sha']=len(lm.get('embedder',{}).get('sha256',''))==64
checks['reranker_pinned_sha']=len(lm.get('reranker',{}).get('sha256',''))==64
checks['qwen_roles']=all('qwen' in lm.get(k,{}).get('id','').lower() for k in ['memory_llm','embedder','reranker'])
checks['gpu_auto_policy']=lm.get('device_policy',{}).get('mode')=='auto' and lm.get('device_policy',{}).get('gpu_layers',0)>=28 and lm.get('device_policy',{}).get('cpu_fallback') is True
checks['cuda_runner_configured']='cuda' in lm.get('cuda_runner',{}).get('id','').lower() and len(lm.get('cuda_runner',{}).get('archives',[]))>=2
checks['cuda_runtime_sha_pinned']=any(len(x.get('sha256',''))==64 for x in lm.get('cuda_runner',{}).get('archives',[]))
checks['nvidia_autodetect']='detectNVIDIAGPU' in text('internal/localinfer/process_windows.go') and 'nvidia-smi' in text('internal/localinfer/process_windows.go')
checks['cuda_cpu_fallback']='runnerCandidates' in text('internal/localinfer/localinfer.go') and 'cpu_fallback' in text('config/local_models.json')
checks['local_model_status_reports_device']='RunnerID' in text('internal/localinfer/localinfer.go') and 'GPUName' in text('internal/localinfer/localinfer.go')

# character source / examples / profile compiler
manifest=jload('package_overlay/ghost/master/character/manifest.json')
checks['character_manifest_v3_shell_scoped']=manifest.get('format_version')==3 and manifest.get('character_file')=='character.md' and 'appearance_file' not in manifest
checks['example_channel_declared']=set(manifest.get('example_files',[]))=={'examples/dialogue.jsonl','examples/interaction.jsonl'}
checks['dialogue_example_channel_exists']=(ROOT/'package_overlay/ghost/master/character/examples/dialogue.jsonl').exists()
checks['interaction_example_channel_exists']=(ROOT/'package_overlay/ghost/master/character/examples/interaction.jsonl').exists()
checks['legacy_reaction_examples_not_packaged']=not (ROOT/'config/reaction_examples.jsonl').exists()
checks['legacy_example_migration']='migrateLegacyReactionExamples' in br and 'Legacy local edits win' in br
checks['examples_are_not_memory']='AUTHOR REFERENCE, NOT MEMORY' in br and 'not past events' in br and 'character/examples' in text('config/README.txt')
checks['example_selector_no_memory_dependency']='characterExampleGuidance' in br and 'canonicalExampleFiles' in br and '8785' not in br and 'rerank' not in br.lower()

checks['character_summary_guide']=bool(text('config/character_summary_guide.md').strip())
checks['profile_compile_endpoint']='/v2/profile/compile' in mem
checks['bridge_profile_hashes']=all(x in br for x in ['SourceHash','GuideHash','ConfigHash'])
checks['summary_rules_shared']='characterSummaryRules()' in br and 'summaryRules(in.Rules)' in mem and 'MaxItemsPerSection' in br and 'MaxItemsPerSection' in mem
checks['summary_no_touch_policy']='Current touch, touch salience' in br and 'Touch model' not in text('package_overlay/ghost/master/profile/generated/character_summary__master.md')
checks['summary_examples_not_compiled']='author-written examples' in text('config/character_summary_guide.md').lower() and 'author examples' in br.lower()
checks['manifest_controls_character_route']='canonicalCharacterFile()' in br and '[CANONICAL CHARACTER DETAIL]' in br and 'read("character/"+chName)' in br
checks['profile_dynamic_state_guard']='touch salience' in br.lower() and 'recent dialogue' in br.lower()

# profile ownership and migration
pp=text('internal/profilepath/profilepath.go')
checks['profile_subtrees']=all(x in pp for x in ['profile", "generated','profile", "state','profile", "settings','profile", "secrets'])
checks['profile_legacy_migration']=all(x in pp for x in ['emotional_state.json','appearance_state.json','touch_state.json','settings.json','credentials.dat']) and 'migration_conflict=' in pp
checks['profile_migration_owner_scoped']=all(x in pp for x in ['func MigrateRuntime','func MigrateTouch','func MigrateCredentials']) and 'MigrateCredentials(root)' in br and 'MigrateRuntime(root)' in rt and 'MigrateTouch(root)' in text('cmd/touchprogress/main.go')
checks['runtime_uses_profile_state']='profilepath.Affect' in rt and 'profilepath.Appearance' in rt and 'profilepath.RuntimeSettings' in rt
checks['touch_uses_profile_state']='profilepath.Touch' in text('cmd/touchprogress/main.go')
cred=text('internal/credential/store_windows.go')+text('internal/credential/store_other.go')
checks['credentials_use_profile_secrets']='profilepath.CredentialsDAT' in cred and 'profilepath.CredentialsJSON' in cred

# linked lifecycle / MCP
for name in ['activate_character_link','get_character_context','begin_character_reaction','request_bridge_reaction','update_character_thinking','begin_character_response','commit_linked_chat','abort_linked_chat','deactivate_character_link']:
    checks['mcp_'+name]=name in link
checks['linked_shared_episode']='EpisodeCommitV2' in linked and 'RequestLinkedChat' in linked and 'commitEpisode(ep)' in linked
checks['linked_affect_canonicalized']='canonicalLinkedReactionEmotion' in linked
checks['linked_secondary_bridge']='RequestPolicy.Secondary = true' in linked
checks['linked_appearance_uses_current_shell_profile']='linkedProfileDocuments' in linked and '/v1/profile/context' in linked and 'state.Appearance' in linked and 'docs.Appearance' in linked
checks['no_cot_tool_surface']='chain-of-thought' in link.lower() and 'scratchpad' in link.lower() and '"chain_of_thought"' not in link.lower() and '"scratchpad"' not in link.lower()
checks['mcp_unified_link_executable']=(ROOT/'cmd/link/main.go').exists() and not (ROOT/'cmd/contextservice').exists() and not (ROOT/'cmd/mcpadapter').exists() and not (ROOT/'cmd/tunnelsetup').exists()
checks['mcp_official_go_sdk']='github.com/modelcontextprotocol/go-sdk/mcp' in link and 'github.com/modelcontextprotocol/go-sdk v1.7.0' in go_mod
checks['mcp_embedded_secure_tunnel']='github.com/openai/tunnel-client' in link and 'github.com/openai/tunnel-client v0.0.11' in go_mod and 'mcp.NewInMemoryTransports()' in link
checks['mcp_no_public_or_local_data_listener']='NewStreamableHTTPHandler' not in link and '/mcp' not in link and 'http.ListenAndServe' not in link
checks['mcp_runtime_is_direct_authority']='http://127.0.0.1:8770' in link and '/linked/turn/commit' in link and '/linked/session/activate' in link
checks['mcp_no_legacy_cloudflare']=all(x not in (link+core+text('scripts/build_windows_amd64.sh')).lower() for x in ['cloudflared','contextservice.exe','mc padapter.exe'.replace(' ',''),'tunnelsetup.exe'])
checks['mcp_runtime_key_not_persisted']='runtime_api_key_env' in link and 'os.Getenv' in link and 'APIKey:              apiKey' in link and 'WriteFile' not in link
checks['mcp_link_gui_only']='build_gui ./cmd/link "$out/Plug/CharacterGPTLink.exe"' in text('scripts/build_windows_amd64.sh') and 'CharacterGPTContextService.exe' not in text('scripts/build_windows_amd64.sh') and 'CharacterGPTMcpAdapter.exe' not in text('scripts/build_windows_amd64.sh') and 'CharacterGPTTunnelSetup.exe' not in text('scripts/build_windows_amd64.sh')
checks['linked_abnormal_release']=all(x in linked for x in ['releaseLinkedPresentation','session_takeover:','abort_already_inactive','deactivate_already_inactive','timeout:']) and 'OnCharacterGPTLinkedRelease' in linked
checks['linked_abort_deactivate_idempotent']='already_inactive' in linked and 'linked turn mismatch' in linked and 'invalid linked session' in linked

# YAYA stable invariants
checks['yaya_v071']='v0.7.1' in core
checks['yaya_fix6_balloon_guard']='CGPT.HasBalloon' in core and 'CGPT.ControlHead' in core and '\\C' in core
checks['yaya_fix7_owl_boot']='\\0\\s[-1]\\1\\s[10]' in core
checks['yaya_link_menu']='ChatGPT連動' in core and 'OnEnableChatGPTConnection' in core
m=re.search(r'CGPT\.BootScript\s*\{(.*?)\n\}',core,re.S); boot=m.group(1) if m else ''
checks['plug_not_started_on_boot']='Plug' not in boot and 'CharacterGPTLink.exe' not in boot
checks['online_update_v07']='https://raw.githubusercontent.com/scread11-maker/charactergpt-update/main/v07/' in text('yaya_adapter/ghost/master/descript.txt')
checks['install_directory_sspgpt_proto_v07']='directory,sspgpt_proto_v07' in text('yaya_adapter/install.txt')
checks['fix9_update_semantics']='TOINT(reference[0]) + 1' in core and "if reference[0] == 'none'" in core

# disk layout / logs
checks['core_executables_grouped']=all(x in core for x in ['bridge/CharacterGPTBridge.exe','bridge/CharacterGPTRuntime.exe','bridge/CharacterGPTTouchProgress.exe']) and 'runtime/CharacterGPTRuntime.exe' not in core and 'touch/CharacterGPTTouchProgress.exe' not in core
checks['memory_service_separate']='memory/CharacterGPTMemoryService.exe' in core
checks['character_manifest_controlled']='canonicalCharacterFile' in br and 'manifest.json' in br and 'safeCharacterFilename' in br
checks['legacy_character_summary_not_consumed']='read("character/summary.md")' not in br and 'filepath.Join(s.root, "character", "summary.md")' not in br
checks['legacy_character_cleanup']=all(x in br for x in ['summary.md','details_.json','empty.md','t.md','LAYOUT_CLEANUP'])
li=text('internal/localinfer/localinfer.go')
checks['inference_logs_unified']='logPath := filepath.Join(m.Root, "logs", "inference", role+".log")' in li and 'migrateLegacyInferenceLogs' in li


# fix8 Memory Recall cleanup
checks['memory_brain_strict_schema']='validateMemoryBrainWire' in mem and 'missing required evaluation object' in mem and 'degenerate all-zero evaluation' in mem
checks['memory_brain_compact_input']='compactMemoryBrainInput' in mem and 'AffectDelta' in mem[mem.index('type memoryBrainInput'):mem.index('type service struct')]
_cons=mem[mem.index('func (s *service) consolidateContext'):mem.index('func canonicalKind')]; checks['invalid_memory_result_not_processed']='runMemoryBrain' in _cons and _cons.index('runMemoryBrain') < _cons.index('processed_v2.jsonl')
checks['legacy_processed_revalidation']='migrateFix8RevalidateProcessed' in mem and 'revalidate_fix8' in mem
checks['recall_depth_presets']=retr.get('presets',{}).get('light',{}).get('candidate_pool')==100 and retr.get('presets',{}).get('medium',{}).get('candidate_pool')==300 and retr.get('presets',{}).get('deep',{}).get('candidate_pool')==600
checks['recall_context_budgets']=retr.get('presets',{}).get('light',{}).get('context_budget_tokens')==512 and retr.get('presets',{}).get('medium',{}).get('context_budget_tokens')==1024 and retr.get('presets',{}).get('deep',{}).get('context_budget_tokens')==2048
checks['candidate_pool_before_rerank']='if len(ids) > pool' in mem and 's.infer.Rerank(deadlineCtx, query, docs)' in mem and mem.index('if len(ids) > pool') < mem.index('s.infer.Rerank(deadlineCtx, query, docs)')
checks['rank_fusion_not_raw_score_merge']='rrfAdd' in mem and '1.0 / float64(rrfK+i+1)' in mem
checks['compact_recall_delivery']='compactRecallMemory' in mem and 'EmbeddingGeneration' not in mem[mem.index('func compactRecallMemory'):mem.index('func addMemoryToCapsule')]
checks['reranker_warmup_outside_recall_budget']='ensureRerankerWarmAsync' in mem and 'reranker warming' in mem and 'semantic_persisted' in mem and 'startup_semantic_store' in mem
checks['recall_menu']='回憶深度' in core and 'OnSetRecallLight' in core and 'OnSetRecallMedium' in core and 'OnSetRecallDeep' in core and 'OnSetRecallUnbounded' in core
checks['recall_depth_ui_owned']='CGPT.RecallDepth' in core and 'CONFIG|RECALL_DEPTH' in core and 'RECALL_DEPTH_SET' in rt and 'recall_depth' not in rt
checks['historical_router_is_general']='最後' in rt and '大改版' not in rt[rt.index('func needsRecall'):rt.index('func normalizeRecallDepth')]
checks['unbounded_replay_preset']=retr.get('presets',{}).get('unbounded',{}).get('replay_max_context_tokens')==32768 and retr.get('presets',{}).get('unbounded',{}).get('candidate_pool') is None
_recall=mem[mem.index('func (s *service) recall'):mem.index('func (s *service) rebuildHot')]; checks['unbounded_bypasses_semantic_inference']='depth == "unbounded"' in _recall and 'replayTail' in _recall and _recall.index('depth == "unbounded"') < _recall.index('s.infer.Embed')
checks['replay_uses_raw_journal']='raw_recent_v2.jsonl' in mem and 'RecallMode = "replay"' in mem and 'Replay' in model
checks['replay_bridge_context_guard']='ContextWindowTokens' in br and 'ContextSafetyMarginTokens' in br and 'fitReplayForPrompt' in br
checks['accepted_input_raw_journal']='/v2/dialogue' in mem and 'user_accepted' in mem and 'recordAcceptedUser' in rt
checks['ssp_user_mirror_is_presentation_only']='buildSSPUserMirrorScript' in rt and 'SSP_USER_MIRROR' in rt and 'return \"\\\\1\" + escaped + \"\\\\e\"' in rt

# fix10 Shell-scoped appearance/profile authority
checks['shell_scoped_appearance_packaged']=(ROOT/'package_overlay/ghost/master/character/appearance_master.md').exists() and not (ROOT/'package_overlay/ghost/master/character/appearance.md').exists()
checks['runtime_owns_shell_key']='ShellKey' in model and 'internal/shellid' in rt and 'shellid.Key' in rt and 'func Key(shellPath, shellName string)' in text('internal/shellid/shellid.go') and 'exactShellKey' in br
checks['per_shell_profile_cache']='character_summary__' in br and 'profileCachePaths' in br and 'AppearanceFile' in model
checks['no_cross_shell_appearance_fallback']='Do not substitute another Shell' in br and 'appearanceFileForShell' in br and 'appearance_master.md' not in br[br.index('func appearanceFileForShell'):br.index('func (s *server) canonicalExampleFiles')]
checks['runtime_shell_change_warms_profile']='/v1/profile/warm' in rt and 'warmBridgeProfile' in rt and 'APPEARANCE_SHELL_CHANGED' in rt
checks['shell_change_triggers_cognition']='submitAppearanceChange' in rt and 'RequestAppearance' in rt and 'CURRENT APPEARANCE CHANGE' in br and 'RequestAppearance' in br
checks['shell_change_initial_discovery_is_silent']='shouldReactToShellChange' in rt and 'previous.ShellKey != ""' in rt
checks['shell_change_respects_linked_primary']='APPEARANCE_COGNITION_SUPPRESSED' in rt and 'linked_turn_active' in rt
checks['appearance_change_cancels_older_request']='activeAppearance' in rt and 'supersedeActiveAppearance' in rt and 'APPEARANCE_COGNITION_SUPERSEDED' in rt
checks['appearance_result_revalidates_current_shell']='appearanceResultMatchesCurrentShell' in rt and 'APPEARANCE_COGNITION_DISCARDED' in rt and 'stale_shell' in rt and 'linked_primary_active' in rt
checks['linked_turn_supersedes_local_appearance']='a.supersedeActiveAppearance()' in linked
checks['dressup_invalidation_uses_shell_identity']='identityChanged := previous.ShellKey != x.ShellKey' in rt and 'if previous.ShellKey != x.ShellKey' in rt
checks['shell_routing_fields_not_sent_to_llm']='promptState.Appearance.ShellPath = ""' in br and 'promptState.Appearance.ShellKey = ""' in br
checks['generated_profile_is_semantic_only']='source_hash:' not in br[br.index('func (s *server) renderProfile'):br.index('func (s *server) renderCanonicalFallback')] and 'Detail routes' not in br[br.index('func (s *server) renderProfile'):br.index('func (s *server) renderCanonicalFallback')]
checks['linked_public_state_hides_shell_routing']='publicLinkedState' in linked and 'out.Appearance.ShellPath = ""' in linked and 'out.Appearance.ShellKey = ""' in linked and '"appearance_file": docs.AppearanceFile' not in linked and '"shell_key": docs.ShellKey' not in linked
checks['memory_brain_appearance_is_semantic_only']='memoryBrainAppearanceInput' in mem and 'PreviousShellKey' not in mem[mem.index('type memoryBrainAppearanceInput'):mem.index('type memoryBrainInput')] and 'CurrentShellKey' not in mem[mem.index('type memoryBrainAppearanceInput'):mem.index('type memoryBrainInput')]
checks['appearance_change_can_enter_episode']='AppearanceChange: job.env.AppearanceChange' in rt and 'AppearanceChange *AppearanceTransition' in model
checks['runtime_boot_not_blocked_by_global_profile']='waitProfileReady' not in rt and 'waitProfileReady' not in linked
checks['yaya_shell_name_from_ssp']='CGPT.ShellName = reference[0]' in core and 'CGPT.ShellPath = reference[1]' in core and 'CGPT.ShellPath = reference[2]' in core
checks['linked_profile_matches_runtime_shell']='/v1/profile/context' in linked and 'state.Appearance' in linked and 'docs.Appearance' in linked
checks['no_unscoped_appearance_migration']='migrateLegacyUnscopedAppearance' not in br and 'appearance.md->character/appearance_master.md' not in br


# fix12 cognition directive layer
dirreg=jload('config/directive_rules.json')
checks['directive_metadata_in_user_input']='type DirectiveRef struct' in model and 'Directive   *DirectiveRef' in model
checks['directive_registry_hot_editable']=(dirreg.get('format_version')==1 and dirreg.get('enabled') is True and 'readmanual' in dirreg.get('directives',{}) and 'ukagaka_en_i' in dirreg.get('directives',{}))
readrule=dirreg.get('directives',{}).get('readmanual',{})
enirule=dirreg.get('directives',{}).get('ukagaka_en_i',{})
checks['directive_canonical_backslash_syntax']=('\\readmanual' in readrule.get('aliases',[]) and '/readmanual' in readrule.get('fallback_aliases',[]) and set(enirule.get('aliases',[])) >= {'えんいー','\\えんいー','\\e'} and '/えんいー' not in enirule.get('aliases',[]))
checks['directive_runtime_recognition_only']='matchDirective' in rt and 'directive.Load' in rt and 'character/manual.md' not in rt
checks['directive_bridge_document_resolution']='directiveGuidance' in br and 'SafeDocumentRelativePath' in br and '[ACTIVE COGNITION DIRECTIVE]' in br and '[DIRECTIVE DOCUMENT:' in br
checks['directive_document_confined_to_character']='directive documents must live under character/' in text('internal/directive/directive.go')
checks['directive_manual_packaged']=(ROOT/'package_overlay/ghost/master/character/manual.md').exists()
checks['directive_memory_policy_modifier']='DirectiveKind' in mem and 'DirectiveID' in mem and 'directive_kind' in text('config/memory_retention_rules.json') and 'directive_id' in text('config/memory_retention_rules.json')
checks['directive_keeps_chat_request_class']='RequestDirective' not in model and 'RequestDirective' not in rt and 'RequestDirective' not in br

# fix13f SSP sent-input mirror cleanup
input_ui_text=text('config/input_ui.json')
checks['backlog_mirror_boolean_default']='"ssp_backlog_mirror": true' in input_ui_text and 'backlogMirror    bool' in rt
checks['backlog_mirror_secondary_scope']='return \"\\\\1\" + escaped + \"\\\\e\"' in rt and '\\p[-100]' not in rt and 'balloonrepaint' not in rt[rt.index('func buildSSPUserMirrorScript'):rt.index('func (a *app) checkFromUI')] and '\\_q' not in rt[rt.index('func buildSSPUserMirrorScript'):rt.index('func (a *app) checkFromUI')]
checks['backlog_mirror_yes_no_menu']='映射送出內容' in core and 'OnSetBacklogMirrorOn' in core and 'OnSetBacklogMirrorOff' in core and 'OnSetBacklogMirrorHidden' not in core and 'OnSetBacklogMirrorOwl' not in core
main_menu=core[core.index('OnCharacterGPTMenu\n{'):core.index('OnCharacterGPTMenuClose')]
checks['backlog_mirror_menu_order']=main_menu.index('OnBalloonDurationMenu') < main_menu.index('OnBacklogMirrorMenu') < main_menu.index('OnRecallDepthMenu')
checks['backlog_mirror_presentation_only']='CHAT|' not in rt[rt.index('func buildSSPUserMirrorScript'):rt.index('func (a *app) checkFromUI')] and r'\![raise' not in rt[rt.index('func buildSSPUserMirrorScript'):rt.index('func (a *app) checkFromUI')]

# fix7 Shell semantic awareness
shellsem=jload('package_overlay/shell/master/sspgpt_semantics.json')
emb=text('cmd/bridge/embodiment.go'); semrt=text('cmd/runtime/shell_semantics.go')
checks['shell_semantics_file_exists']=shellsem.get('format_version')==1 and shellsem.get('default_pose')=='normal'
checks['shell_semantics_has_hand_to_chin']=any(x.get('id')=='hand_to_chin' for x in shellsem.get('poses',[]))
checks['shell_semantics_has_thinking']=any(x.get('id')=='thinking' for x in shellsem.get('poses',[]))
checks['request_envelope_carries_embodiment']='Embodiment' in model and 'EmbodimentCapabilities' in model
checks['bridge_receives_bounded_shell_semantics']='CURRENT SHELL EMBODIMENT SEMANTICS' in br and 'embodimentGuidance' in emb
checks['presentation_schema_requires_pose']='"pose"' in emb and 'required' in emb and 'presentationSchema(env)' in br
checks['runtime_resolves_pose_not_gesture']='resolveSemanticSurface' in semrt and 'Presentation.Gesture' not in semrt
checks['linked_context_exposes_embodiment']='"embodiment": embodiment' in linked
checks['no_surface_ids_in_bridge_semantics']='Surfaces' not in emb and 'surface115' not in br

# fix6 coordinated graceful shutdown
checks['runtime_http_shutdown_coordinator']='mux.HandleFunc("/shutdown"' in rt and 'beginShutdown("http")' in rt and 'shutdownServices' in rt
checks['runtime_shutdown_memory_last']='{"MemoryService", "http://127.0.0.1:8768/shutdown"' in rt and rt.index('http://127.0.0.1:8768/shutdown') > rt.index('http://127.0.0.1:8767/shutdown')
checks['yaya_close_uses_runtime_http']='http://127.0.0.1:8770/shutdown' in core and 'SYSTEM|CLOSE' in core
checks['shutdown_idempotent']=all('shutdownOnce' in x for x in [rt,br,mem,touch])
checks['bridge_shutdown_cancels_inflight']='SHUTDOWN_BEGIN inflight=' in br and 'cancel()' in br
checks['touch_shutdown_persists']='SHUTDOWN_BEGIN persisted=true' in touch and 'persistLocked()' in touch
checks['memory_shutdown_requeues_unprocessed']='requeueUnprocessed' in mem and 'retry_on_next_start' in mem
checks['memory_shutdown_stages_before_persist']='prepareSemantic(parent' in mem and 'persistPreparedSemantic' in mem and 'cancel_inference_preserve_persistence' in mem
checks['memory_shutdown_waits_before_runner_stop']='s.opsWG.Wait()' in mem and mem.index('s.opsWG.Wait()') < mem.index('s.infer.Stop()')

warnings={}
main_cuda=(lm.get('cuda_runner',{}).get('archives') or [{}])[0]
warnings['cuda_main_runner_sha_unpinned']=len(main_cuda.get('sha256',''))!=64
warnings['cpu_main_runner_sha_unpinned']=len(lm.get('runner',{}).get('archive_sha256',''))!=64

report={
 'version':'0.7.1-fix15-mcp',
 'source_root':str(ROOT),
 'checks':checks,
 'passed':all(checks.values()),
 'failed':[k for k,v in checks.items() if not v],
 'known_followups':[k for k,v in warnings.items() if v],
 'sha256':{
  'runtime':'', 'bridge':'', 'memory':'', 'touch':'',
  'cgpt_core.dic':sha('yaya_adapter/ghost/master/cgpt_core.dic'),
  'cgpt_touch.dic':sha('yaya_adapter/ghost/master/cgpt_touch.dic'),
 }
}
for key,p in [('runtime','dist/windows_amd64/core/CharacterGPTRuntime.exe'),('bridge','dist/windows_amd64/core/CharacterGPTBridge.exe'),('memory','dist/windows_amd64/core/CharacterGPTMemoryService.exe'),('touch','dist/windows_amd64/core/CharacterGPTTouchProgress.exe')]:
 if (ROOT/p).exists(): report['sha256'][key]=sha(p)
print(json.dumps(report,ensure_ascii=False,indent=2))
sys.exit(0 if report['passed'] else 1)
