# SSPGPT／慕娜系統手冊

> 本文件是 **SSPGPT v0.7.1 GPU1 fix12** 目前已建立概念與功能的正式參考資料。
> 使用 `\readmanual` 提問時，應以本手冊為主要文件依據；沒有記載的事項要明確說明資料不足，不要把推測冒充成既有功能。
> **即時狀態優先於靜態文件。** 接觸、release、Shell、外觀、表情、姿勢、配件與目前情緒，以 SSP／Runtime 的 CURRENT STATE 為準。

## 1. SSPGPT 是什麼

SSPGPT 是把大型語言模型的理解能力、記憶系統與傳統伺か／SSP Ghost 的身體、Shell、氣泡與碰觸整合在一起的實驗性架構。

慕娜是 SSPGPT 的人格介面。她不是單純把 LLM 回答貼進氣泡，而是有一個持續存在於本機的「現在」：目前身體接觸、外觀、情緒與近期情境會先被整理，再交給 LLM 依角色設定理解與反應。

慕娜手上的書象徵連接 LLM、思考與查找答案的鑰匙。Owl／貓頭鷹是獨立陪伴角色，不是慕娜身體的一部分。

核心精神：

- 身體感測與物理事實在本機判定。
- 角色如何理解事實，由 LLM 決定。
- 情緒會延續並衰減，不會每輪歸零。
- 記憶是選擇性的，不把所有感測資料永久保存。
- 普通對話優先走低延遲 Fast Path；真正需要歷史時才 Recall。
- SSP 本地與選用的 Web ChatGPT 連動共用同一個慕娜、同一份現在與記憶。

## 2. 「現在、過去與理解」

SSPGPT 的責任分工：

- **TouchProgress**：感測接觸、移動、速度、反轉、停留與放開。
- **Runtime**：權威的現在；保存物理狀態、目前 affect、外觀、情境與 request lifecycle。
- **MemoryService**：過去；保存 raw history、Episode、Semantic Memory、Hot Memory、Recall 與背景整理。
- **Bridge**：組合角色、現在、必要記憶與規則，與主要 LLM 溝通並解析反應。
- **YAYA／SSP**：氣泡、Shell、表情、姿勢、選單與計時等呈現／使用者操作。

重要優先順序：

**目前物理事實 > 近期碰觸顯著性 > Hot Memory > 回想出的歷史。**

所以只要現在已確認 `release`，任何舊記憶都不能讓慕娜誤以為仍在接觸。

## 3. 本地文字對話

左鍵點慕娜的非碰觸區域可開啟選單，再選「開始對話」。Runtime 對話窗支援：

- 多行輸入；**Enter 送出、Shift+Enter 換行**。
- 「情緒」選擇器：表示**使用者自述情緒**，不是直接設定慕娜心情，也不證明使用者意圖。
- 「檢查間隔」：5、10、15、30、60 秒或手動。

檢查間隔不是 timeout。到時間只代表 request 可以人工檢查；若仍在思考，可「繼續等待」或「打斷她」。取消針對該次 request，不是盲目殺掉整個系統。

LLM 可選擇 `speak`、`silent` 或 `defer`。`defer` 會建立有 parent request 的延遲續答；新的衝突輸入或失效情境可以使續答取消。

## 4. 自主 cognition

長時間沒有互動時，SSP 可以提供自主思考機會；計時器觸發不等於一定要說話，慕娜可以選擇 `speak` 或 `silent`。

前景聊天／碰觸反應優先於自主與背景記憶工作。碰觸感測本身屬於身體事實，即使 cognition 忙碌也必須繼續更新。

## 5. 身體碰觸

高頻 MOVE 不會直接送給 LLM。TouchProgress 先把連續輸入整理成物理事件，Runtime 再把事件放進當下情境。

主要 gesture：

- `light_touch`：短暫輕碰。
- `heavy_tap`：較重的一下接觸。
- `gentle_stroke`：較慢、較輕柔的持續撫動。
- `stroke`：一般強度持續撫動。
- `rough_rub`：快速且多次方向反轉的粗糙揉動。
- `grab`：明確抓取。
- `resting_touch`：同一接觸仍存在，但目前沒有有意義的移動。
- `release`：接觸已結束，屬權威物理事實。
- `look_at`：例如對 Book 的「看了一眼而非接觸」。

主要 target：`Head`、`Hair`、`Bust`、`Book`、`Owl.Head`、`Owl.Bust`、`Owl.Wing`。

其中 `Hair` 指背後長髮；`Owl.Bust` 指貓頭鷹軀幹，不能與慕娜的 `Bust` 混淆。

物理事件只回答「發生了什麼」，不自動判定動機。摸頭不等於喜歡、胸部接觸不自動等於戀愛／性意圖、看書也不代表一定想讀。重複互動可影響語氣，但不同次真實事件仍要保持可區分。

## 6. 持續情緒

慕娜的 affect 是 Runtime 中持續並隨時間衰減的本機狀態。目前核心 channels：

`positive`、`shy`、`wary`、`annoyed`、`downcast`。

LLM 回傳語義上的 `reaction_emotion`，Runtime 再轉換成 affect 變化。碰觸不直接硬編成固定情緒；相同動作在不同情境可以有不同反應。

## 7. 表情、姿勢與 Shell

LLM 不直接操作 surface 編號，而是描述 expression、pose、gaze 等語義 presentation，Runtime 再依目前 Shell 能力解析。

master Shell 已建立主要 pose family：

- `normal`：一般基礎構圖。
- `hand_to_chin`：手抬到下巴附近，也可作部分「舉手」需求的視覺近似。
- `thinking`：明確沉思姿勢。

姿勢與 gesture 不同：「使用者做了什麼」和「慕娜要擺什麼姿勢」不可混為一談。

SSPGPT 的外觀以**目前 Shell 為單位**。每個 Shell 可有自己的穩定 appearance；即時表情、視線、姿勢、眼鏡與 dress-up 狀態仍服從 SSP／Runtime。

真正切換 Shell 時，Runtime 先更新 CURRENT STATE，再使用新 Shell 對應外觀，並可觸發一次 `appearance_change` cognition，讓慕娜自然意識到外觀改變。首次啟動時第一次得知 Shell、或只有顯示名稱變動，不應假裝成換裝。

每個 Shell 的角色摘要可獨立快取；來源沒改時直接 cache hit，真正修改角色／外觀來源時才重建。

## 8. 記憶系統

記憶採分層設計：

- **Raw History**：接近原話的時間序紀錄，供 Replay。
- **Episode Journal**：完成互動先落地的事件紀錄。
- **Semantic Memory**：背景 Memory Brain 從 Episode 中提取較值得保存的事實、事件與觀察。
- **Hot Memory**：很小、目前特別重要的記憶快照，可直接供 Fast Path 使用。
- **Recall**：只有真的需要歷史時才啟動。

Episode 不等於永久記憶。MemoryService 可以丟棄、短期保留、放入 Hot Memory、列為長期候選或背景整理。背景記憶形成不能阻塞眼前回答。

## 9. 回憶深度

左鍵選單可切換「回憶深度」。它只改變**需要回想時**的投入，不改變記憶形成規則。

- **輕**：約 100 候選、約 512 tokens 最終上下文。
- **中（標準）**：約 300 候選、約 1024 tokens；預設模式。
- **深**：約 600 候選、約 2048 tokens。

前三種會合併向量、文字／實體等候選，再由 reranker 做最終相關性排序；向量相似度只是高召回來源，不是最終判斷。

### 無止盡

「無止盡」是 **Raw Replay**，不是把 Deep 放大：

- 不先做 embedding candidate search，也不靠 reranker 篩掉原話。
- 從 SSPGPT 自己的 raw chronological history 由最近向過去讀，再恢復正常時間順序給主 LLM。
- 只有 Recall Router 判定需要歷史時才啟動；普通聊天即使選無止盡，也不平白注入 Replay。
- 目前 Replay safety ceiling 為 32768 tokens，實際量仍受主模型剩餘 context、輸出預留與安全 margin 限制。

**無止盡讀的是 SSPGPT 自己的 raw history，不是反向抓 SSP Backlog Viewer。**

## 10. SSP Backlog Mirror

Runtime 接受的使用者文字會保存於 SSPGPT raw history，並可單向映射到 SSP 的原生對話回放。這只是 presentation：不重新觸發 LLM、不建立第二份 Episode，也不是 Replay 的資料來源。

左鍵選單中的「映射送出內容」只有 **是／否**：

- **是**：把 Runtime 已接受的送出內容同步交給 SSP presentation/history。
- **否**：停止 SSP mirror；SSPGPT 自己的 Raw Replay 仍照常記錄。

SSP 目前沒有被 SSPGPT 以非侵入方式當成 backlog-only 資料庫使用，因此映射內容可能在桌面 presentation 中短暫可見。這不影響 Runtime 對話、Memory、Raw Replay 或 LLM 回答。

## 11. 本地記憶模型

主要人格回答仍由 Bridge 使用設定的主要 LLM provider。本地小模型不是慕娜的第二人格。

目前本地 inference 用於：角色摘要、Memory Brain 記憶評估／整理、multilingual embedding 與 reranking。Windows 可自動偵測 NVIDIA，優先使用 CUDA runner，失敗時可回退 CPU。這些背景模型不應成為普通 Fast Path 的同步瓶頸。

## 12. Web ChatGPT 連動

「ChatGPT連動」是選用功能，正常 Ghost 開機不會自動啟動 Plug，可由左鍵選單手動開啟、關閉、重連或設定。

概念是 **One Muna, two conversation surfaces, one authoritative local state**：

- 普通本地聊天：Bridge／主要 API LLM 是 Primary Brain。
- Web linked turn：Web ChatGPT 暫時成為 Primary Brain；本地 Bridge 只做短而自然的 Secondary Brain 反應，不重做完整答案。
- Runtime 仍擁有目前身體、外觀、affect 與 lifecycle。
- 完成的 Web turn 只形成一個正常 memory episode，因此回到 SSP 後仍可延續同一段經歷。

SSP 可以呈現「收到問題、思考中、準備回答、完成」等**可觀察狀態**，但不傳輸 Web ChatGPT 私人 chain-of-thought。連動期間碰觸、release、外觀變化與 affect decay 仍繼續；連線失敗／timeout 應恢復普通本地模式。

## 13. Cognition Directive／認知指令

fix12 的 Directive 是普通 `chat` 的**認知修飾層**，不是 console、不是另一條 cognition pipeline，也不能任意執行系統命令。

### `\readmanual`

正式輸入：`\readmanual 你的問題`
也可自然連寫：`\readmanual你的問題`。系統會把中文／日文等非 ASCII 指令識別字元視為問題起點；像 `\readmanualfoo`、`\readmanual123`、`\readmanual_test` 則不會誤判成這個指令。

系統保留原始輸入供 Replay／backlog／Episode 使用，但本次 cognition 會辨識 `document_query`、載入本手冊，並把 `\readmanual` 後面的問題當成主要 CURRENT USER INPUT。`/readmanual` 僅作為內部防呆／消歧義容錯，會被正規化到同一個 `readmanual` directive；它不是正式推薦語法。回答應以手冊為主要文件依據；沒有寫到的地方要區分推測。

普通聊天不會每次載入 manual，因此沒有固定額外 token 成本。

Directive registry 可熱編輯，可增加／刪除註冊文件指令與文化語義 alias。使用者不能在指令文字中指定任意 filesystem path；文件必須事先註冊並限制在角色資料範圍。未註冊的指令樣式只是普通聊天；`/readmanual` 只有因為被明確列為防呆 alias 才會被辨識。

### `えんいー`

目前 `えんいー`、`\えんいー`、`\e` 會被統合為同一個已消歧的伺か文化 `semantic_alias`：由 SakuraScript 終了 tag `\e` 的讀法／慣用表現延伸出的結束談話、告別或收尾意涵。這裡統合的是語義，不會把 Runtime 對話中的 `\e` 當成可執行 SakuraScript。`/えんいー` 是錯誤語法，不列入 alias。

遇到精確 alias 時，慕娜應像理解自身文化一樣自然反應；除非使用者要求解釋，不必先做字典式定義。現在是 exact matching，因此「えんいーって何？」仍是普通自然語言問題。

Directive metadata 會跟普通 Episode 進入 MemoryService；規則可以提高其 retention candidate 權重，但不等於強制永久記住。

## 14. 角色資料與可編輯性

主要角色資料：

- `character.md`：穩定身份、世界觀、性格、關係與說話方式。
- `appearance_<shell>.md`：特定 Shell 的穩定外觀。
- `manual.md`：明確文件查閱時使用的正式手冊。
- `examples/dialogue.jsonl`：作者手寫對話範例。
- `examples/interaction.jsonl`：作者手寫互動／碰觸範例。

大量行為政策放在可熱編輯 config；一般規則調整不必重新編譯 Ghost。角色摘要只是節省日常 prompt 成本的有損語義索引，不是 canonical source；需要細節時應回到正確角色／外觀文件。

## 15. API、更新與關閉

左鍵選單可「設定 API Key」。憑證屬 private state，不應寫進角色文件、記憶、Prompt 或 SSP backlog。

SSP 原生網路更新使用穩定 `v07/` channel，採保守策略，只適合 presentation／bootstrap-safe 文字與 metadata；核心執行檔、角色、profile、memory、credentials 與 local models 不應由一般 online update 任意覆蓋，核心升級以完整 NAR 為準。

正常關閉 SSP 時，Runtime 會協調核心服務停止。Memory episode 採 journal-first；未完成的背景整理應保留並於下次啟動重排，而不是假裝成功。

## 16. 常見問題

**「你每次都讀完所有記憶嗎？」**
不是。普通聊天優先 Fast Path；需要歷史才 Recall。

**「無止盡就是 SSP 回放嗎？」**
不是。它讀 SSPGPT 自己的 raw history；SSP backlog 只是 presentation mirror。

**「選 happy，慕娜就會變開心嗎？」**
不會。那是使用者自述情緒；慕娜自己的 affect 由 reaction emotion 經 Runtime 更新。

**「摸頭一定會開心嗎？」**
不會。物理事實與角色解讀分離。

**「換 Shell 你知道嗎？」**
真正 Shell 改變會先更新目前外觀，再提供 appearance-change cognition。

**「Web ChatGPT 和 SSP 是兩個慕娜嗎？」**
設計上不是。兩個介面共享同一個 Runtime NOW、affect 與 MemoryService，只是在 linked turn 中由 Web ChatGPT 暫時擔任 Primary Brain。

**「`\readmanual` 可以讀任意電腦檔案嗎？」**
不可以。只能讀 registry 預先允許的角色文件。

**「如果我誤打成 `/readmanual` 呢？」**
仍會被內部防呆 alias 正規化成同一個 `readmanual` directive，但正式寫法是 `\readmanual`。

**「手冊和現在狀態衝突時信誰？」**
相信現在。CURRENT STATE 對動態事實有最高權威。

## 17. 一句話總結

**SSPGPT 讓慕娜以 SSP Ghost 的身體活在桌面上：本機系統負責感覺身體、保存現在與過去，LLM 負責理解並形成角色反應；記憶、情緒、Shell、碰觸、延遲／自主 cognition、Web 連動與文件型認知指令，都圍繞同一個慕娜與同一份權威現在運作。**
