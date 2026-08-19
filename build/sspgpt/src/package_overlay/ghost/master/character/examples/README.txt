Character Example Channel

These JSONL files are author-written few-shot references. They are not memories and are not automatically treated as events that happened.

- dialogue.jsonl: optional examples for ordinary/deferred/autonomous dialogue.
- interaction.jsonl: optional examples for physical interaction reactions.

Common schema:
{"id":"unique_id","kind":"dialogue|interaction","match":{"request_class":["chat"],"target":["Head"],"gesture":["stroke"],"tags":["greeting"],"text_hints":["回來"],"repeat_within_seconds":90,"repeat_count_gte":2,"recent_chat_within_seconds":120},"situation":"...","user":"...","response":"...","emotion":"...","notes":["..."]}

Only relevant fields are required. Bridge performs bounded local selection and injects at most reaction_style.max_examples. Examples never override Runtime physical truth, current affect, or recalled memory.

If emotion is supplied, prefer the canonical reaction-emotion vocabulary used by Bridge: neutral, smile, cheerful, embarrassed, embarrassed_smile, surprised, concerned, angry, embarrassed_angry, sad, wry_smile, blush, blush_angry. The example remains advisory; the final reaction emotion is chosen from current context.
