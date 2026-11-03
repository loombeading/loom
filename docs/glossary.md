# 用語集

Loom で用いる中核概念と用語について、その定義、状態遷移や関係、対応するコマンドをまとめています。用語は、Loom 本体が保存して扱う語と、Loom を使う実行者の側の語に分けて載せています。

語の定義はこのページが正です。`--severity` のようなフラグ名の見出しへは、`glossary.md#--severity` のように先頭の `--` を含めた形でリンクできます。

## Loom 本体

`lm` が保存し、コマンドの出力に現れる語です。

### Bead
- 一言の定義: Loom で作業・タスク・Gate・Epic を管理するときの最小単位です。
- 状態遷移や関係: `open`（未着手）から `in_progress`（着手）を経て `closed`（完了）へ進みます。やらないと判断したものは `cancelled`（取りやめ）にします。`closed` と `cancelled` が終端状態で、[依存](#依存)によってブロックされることがあります。
- 出てくるコマンド: `lm create`, `lm show`, `lm list`, `lm close`

### 依存
- 一言の定義: Bead 同士の先行・後行の関係を表す有向の辺です。辺をたどって元の Bead に戻る循環は作れないので、依存の全体は有向非巡回グラフ（DAG・Directed Acyclic Graph）になります。
- 状態遷移や関係: `blocks`（先行 Bead が閉じるまで後行 Bead は Ready にならない）・`parent-child`（親子関係）・`discovered-from`（作業中に見つけた派生課題の出どころ）・`duplicates`（同じ作業を二度起票した重複。関連するだけの別の作業には使いません）の 4 種があります。このうち Ready を左右するのは `blocks` と、終端でない子を持つ親の `parent-child` です（子がすべて終端になると親が Ready に出ます）。循環する依存は拒否されます。
- 出てくるコマンド: `lm dep add`, `lm dep remove`, `lm create --blocked-by <ID>`（起票と同時に `blocks` を張る。繰り返し指定できる）, `lm create --parent <ID>`, `lm create --discovered-from <ID>`

### 監査ログ
- 一言の定義: Bead の状態や項目を変えた操作を、操作した actor・時刻・理由（`--reason`）とともに 1 件ずつ残した履歴で、データベースの audit_log 表に保存されます。
- 状態遷移や関係: 記録は追記だけで、項目の変更・依存の追加と除去・claim と解放などを残します。理由だけを残したいときは、ほかのフラグを付けずに `lm update <id> --reason "..."` を打ちます。`lm show` の `## History` に出て、全文は `--full` で読めます。`lm export` の JSONL には含まれないので、`lm import` は監査ログを取り込みません。`lm import` が Bead・依存・消費トークン量を手元に書き加えたときは、その書き加え自体を取り込み由来の記録として新しく残し、手元で書いた記録と区別します。export と import で DB を作り直すと、元の監査ログは失われます。
- 出てくるコマンド: `lm show <id>`, `lm show <id> --full`, `lm update <id> --reason "..."`

### 実効優先度
- 一言の定義: 保存した優先度に、割り込み・期限・前提や親子の関係による引き上げを重ねて導いた、並びに使う優先度です。
- 状態遷移や関係: 割り込みを持つ Bead は `P0`、期限の手前に入った Bead は `P1` になり、引き上げは塞いでいる前提と子にも伝わります。[時間調整](#時間調整)が効いているときは、経過時間による昇格（`aged`）と減衰（`decayed`）も重なります。`lm ready`・`lm blocked`・`lm gate`・`lm list --sort priority` はこの値で並び、保存値と違う行の末尾に `eff=P<n>(<理由>)` が付きます。`lm show` では `effective_priority` と `effective_priority_source` に出ます。理由ごとの読み方は [skills/loom/docs/priority.md](../skills/loom/docs/priority.md)「`eff=` の読み方」にあります。
- 出てくるコマンド: `lm ready`, `lm list --sort priority`, `lm show`, `lm dep add --dry-run`

### Ready
- 一言の定義: 未着手（`open`）でブロッカーが無く、終端でない子も持たず、今すぐ着手できる Bead の状態です。
- 状態遷移や関係: 先行するブロッカーが `lm close` や Gate の resolve で解消されると、それに依存していた後続の Bead が Newly ready になります。子を持つ親は、子がすべて終端（`closed`・`cancelled`）になると Ready に出ます。
- 出てくるコマンド: `lm ready`, `lm ready --claim`, `lm ahead`

### close
- 一言の定義: 作業を終えた Bead を完了（`closed`）に移す操作で、要約と理由を添えて記録できます。
- 状態遷移や関係: `in_progress`（または `open`）から `closed` へ進みます。閉じた Bead が塞いでいた後続は、他にブロッカーが無ければ Newly ready になります。`--summary`（要約）と `--reason`（理由）はどちらも任意で、`--summary` を付けると閉じたあとの `lm show` で本文の代わりに要約が出ます。取りやめるときは close ではなく `--status cancelled` を使います。
- 出てくるコマンド: `lm close <id> --summary "..." --reason "..."`

### Newly ready
- 一言の定義: 直前の操作（close・Gate の resolve / reject・前提の取り消しなど）でブロッカーが外れ、新たに Ready になった Bead のことです。
- 状態遷移や関係: 操作の出力の `## Newly ready` 節に一覧され、次に何を claim するかの手がかりとして使います。
- 出てくるコマンド: `lm close`, `lm gate resolve`, `lm gate reject`, `lm update <id> --status cancelled`

### claim
- 一言の定義: Ready な Bead を特定の作業者（actor）が引き受け、状態を `in_progress` に移す排他的な操作です。
- 状態遷移や関係: `open` から `in_progress` へ進みます。完了したときは `lm close` で閉じ、中断して手放すときは `--release` で `open` に戻します。claim した actor は `claimed_by` に、claim の期限は `claim_expires_at` に残ります。期限は `--claim` に添える `--claim-ttl`（`30m`・`2h` のような正の時間。`--claim` なしでは拒否されます）で決めます。`--claim-ttl` を省いた claim には期限が付かず、期限切れで引き継がれることもないので、止まった actor の claim は `--release --force` で解放します（[skills/loom/docs/claim-recovery.md](../skills/loom/docs/claim-recovery.md)）。期限が切れた claim は別の actor が引き継げます（引き継ぎは監査ログに `claim_takeover` として残ります）。`--force` は、他の actor の claim を `--release` で解放するときと、`--claim` と `--reason` を添えて未完了の `blocks` を越えて claim するときに使います。
- 出てくるコマンド: `lm ready --claim [--claim-ttl 30m]`, `lm update <id> --claim [--claim-ttl 30m]`, `lm update <id> --release`, `lm update <id> --release --force`, `lm list --claim-expired`

### Gate
- 一言の定義: 自動の進行を止め、人間の承認や外部条件の成立を待つための特殊な Bead です。
- 状態遷移や関係: `open`（判断待ち・条件待ち）の Gate は、`resolve`（合流・許可）で後続を unblock するか、`reject`（取り下げ）で後続ごと破棄します。待ち条件は `kind`（human / confirm / adjudicate / external）や `wait:` ラベル（外部 PR の merge など）で宣言します。`human` は人が判断して開く Gate、`confirm` は利用者が定めた機械判定（テスト・lint 等）が全て通ったときに開く確認型の Gate で、どちらも閉じた `adjudicate` の Gate を `--adjudicated-by` に渡さないと作れません（詳しくは [skills/loom/docs/gate.md](../skills/loom/docs/gate.md)）。`wait:` ラベルを測って resolve するのは実行者か利用者で、`lm` は測りません。`lm close` と `lm gate sweep` が自動で resolve するのは、塞いでいる Bead が 1 件以上あり全て終端（closed・cancelled）になった Gate だけです。
- 出てくるコマンド: `lm gate`, `lm gate create`, `lm gate resolve`, `lm gate reject`, `lm gate sweep`

### Gate の節
- 一言の定義: `lm gate` が open の Gate を、開けるのは誰かで分けて出す節の名前です。`lm ahead` の `## Gate waiting` も同じ分け方を使います。
- 状態遷移や関係: 節は次の 5 つで、上から順に出ます。空の節は出ません。

  | 節の名前 | 入る Gate | 開けるのは |
  |---|---|---|
  | Needs you | `kind:human`・`kind:confirm` で、前提が残っていないもの | あなた（`lm gate resolve` か `lm gate reject`） |
  | Blocked by other work | `kind:human`・`kind:confirm` で、Gate 自身に未完了のブロッカーがあるもの、または終端でない塞ぎ先のどれにも他の未完了のブロッカーが残るもの（終端の塞ぎ先は判定に使いません） | 前提が片付いたあとのあなた（それまでは Needs you に出ません） |
  | Waiting on judge | `kind:adjudicate` | [判定者](#判定者)の結論に沿って、判定者を起動した側 |
  | Opens automatically | `kind:external` | `wait:` ラベルの条件を測る実行者や [watcher](#watcher) |
  | Unknown kind (fix labels) | `kind:` ラベルの無い Gate（インポート等で持ち込まれたもの） | まだ誰も開けません。`lm update <id> --add-label kind:adjudicate` か `kind:external` でラベルを付けます |

  `lm ahead` は `### Needs you` にだけ Gate ごとの行を出し、他の節は `- Opens automatically: 1 gates ...` のように件数の 1 行に畳みます。あなたの操作を待っている Gate は Needs you の行だけです。

  `lm ahead` の `## Gate waiting` には、節とは別に `- Stalled: <Gate の ID> (wait:date passed but still open)` の行が出ることがあります。Stalled は `wait:date:` の日付を過ぎてもまだ open の Gate を並べた行で、`wait:` ラベルの条件を測る実行者や [watcher](#watcher) が止まっている兆しです。
- 出てくるコマンド: `lm gate`, `lm ahead`

### Gate の持ち主
- 一言の定義: その Gate を作った actor です。Gate の履歴（`lm show <id>` の `## History`）の `created` の行に記録された actor がそのまま持ち主になります。
- 状態遷移や関係: actor は `--actor` で決まり、無いときは環境変数 `LM_ACTOR`、それも無いときは `CLAUDE_CODE_SESSION_ID`、それも無いときは OS のユーザー名の順で決まります。持ち主を表す別の印は要りません。
- 出てくるコマンド: `lm gate create`, `lm show <id>`

### 孤児 Gate
- 一言の定義: 持ち主のセッションが終わったまま `open` に残った `kind:external` の Gate です。
- 状態遷移や関係: 誰も resolve しないので、放っておくと下流の Bead を塞ぎ続けます。実行者は持ち主の actor が今も動いているかを測り、止まっていると確かめられたときだけ `lm gate resolve` し、確かめられないときは根本原因の Bead を起票します。何をもって「動いている」とするか（プロセスの生存・監査ログへの直近の書き込みの時刻・claim の期限など）は Loom が定めず、利用者が決めます。
- 出てくるコマンド: `lm gate`, `lm gate resolve`, `lm show <id> --full`

### Epic
- 一言の定義: 複数の子タスク（Bead）を束ねる、大きめの親タスクの単位です。
- 状態遷移や関係: 子の Bead とは `parent-child` の依存で結ばれます。`milestone:<名前>` ラベルを付けると、マイルストーンとして扱えます。Epic 専用の type はありません。Epic は `--type` を付けずに `lm create --title "<Epic の名前>"` で作る、type が `task` の Bead で、子を `--parent <Epic ID>` で結ぶと Epic として働きます。`--type`（`lm list --type` も同じ）が取る値は `task`（既定）と `gate` の 2 つで、`gate` の Bead は `lm gate create` で作ります。
- 出てくるコマンド: `lm create --title "<Epic の名前>"`, `lm create --parent <Epic ID>`, `lm list --parent <Epic ID>`, `lm ahead`

### マイルストーン
- 一言の定義: 出荷版や到達目標ごとに Bead 群をまとめた到達点です。出荷版かどうかは名前で表します（例: `milestone:v0.2610.0`）。
- 状態遷移や関係: Epic などに `milestone:<名前>` ラベルを付けるとマイルストーンとして認識され、`lm ahead` の `## Milestones` で残数が追跡されます。
- 出てくるコマンド: `lm create --label milestone:<名前>`, `lm update <id> --add-label milestone:<名前>`, `lm ahead`

### course
- 一言の定義: 段のことで、Ready をすべて閉じたときに `blocks` が外れて Ready になる Bead の集合を指します。
- 状態遷移や関係: Ready の次が Course 1、その次が Course 2 と続き、`lm ahead` が段ごとにトークン消費の見積もりを集計します。
- 出てくるコマンド: `lm ahead`

### wip
- 一言の定義: 現在着手中（`in_progress`）の Bead、またはその件数（Work In Progress）を指します。
- 状態遷移や関係: `claim` されると wip になり、`close` または `release` されるまでその状態が続きます。
- 出てくるコマンド: `lm ahead`, `lm list --status in_progress`

### Compaction
- 一言の定義: 終端状態になってから保持期間を超えた Bead について、出力する項目を減らしてトークン消費を抑える仕組みです。
- 状態遷移や関係: 保持期間は、要約を持って終端状態になった Bead の本文を `lm show` が省くまでの日数です。既定は 0 日で、要約を付けて閉じた Bead はすぐに本文が省かれます。日数は `config.json` の `compaction.retention_days` で延ばせます（置き場所と書式は [skills/loom/docs/env.md](../skills/loom/docs/env.md)「設定ファイル（`config.json`）」）。表示上の投影なので、保存データは書き換えません。本文の代わりに要約（`--summary`）が出ますが、全文は `--full` で読めます。
- 出てくるコマンド: `lm close --summary "..."`, `lm show <id> --full`

### namespace
- 一言の定義: 同じ Loom データベース（`loom.db`）の中で、複数のプロジェクトや領域を見分けて分けるための接頭辞です。
- 状態遷移や関係: Bead の表示 ID の接頭辞（既定は `id-`）を切り替えるので、プロジェクトごとに一覧や検索を絞り込めます。
- 出てくるコマンド: `lm create --namespace <名前>`, `lm list --namespace <名前>`

### token cost
- 一言の定義: エージェントがタスクを進めるうえで消費したモデルのトークン数、または費用の記録です。
- 状態遷移や関係: `lm cost add <id> --in <入力トークン数> --out <出力トークン数>` で Bead ごとに記録して積算され、`lm show` の `token_cost` に出ます。`lm ahead` は close 済みの Bead に記録された値の中央値から、Ready と後続の段の消費を見積もります。記録しなくても見積もり以外の操作には影響しません。
- 出てくるコマンド: `lm cost add`, `lm show`, `lm ahead`

### rework
- 一言の定義: 作業の差し戻し（CI の失敗・レビュー指摘による追いコミット・後追いの修正）を 1 回ごとに Bead へ記録したものです。原因は `--cause` に `ci`・`review`・`followup` のどれかで書きます。
- 状態遷移や関係: Loom は GitHub などの外部を読まないので、差し戻しは記録しない限り数えられません。記録した件数は `lm ahead --calibration` の `## Rework` 節に `reasoning_depth` 別の差し戻し率として出て、見積もりや分解の粒度を見直す材料になります。記録しなくてもほかの操作には影響しません。
- 出てくるコマンド: `lm rework add <id> --cause <ci|review|followup>`, `lm ahead --calibration`

### external-ref
- 一言の定義: Bead に関連付けた外部リソース（PR の URL や引き継ぎ簿のパスなど）への参照です。
- 状態遷移や関係: 作業の所在や追跡対象を示すもので、PR の状態など外部の状況を確かめるときの手がかりに使われます。
- 出てくるコマンド: `lm update <id> --external-ref <url|path>`

### --severity
- 一言の定義: Bead の重大度で、事実を表す 1〜4 の整数です。`1` は停止、`2` は劣化、`3` は通常（既定）、`4` は軽微です。
- 状態遷移や関係: 起票時に証拠を添えて付け、迷えば上（小さい値）を選びます。変えるときは `--reason` に実測を書きます。重大度は並びに直接は使わず、[時間調整](#時間調整)の昇格の上限・減衰の下限・保留の有無を決めます（上限と下限の段は[係数](#係数)の値です）。時間で重大度が変わることはありません。`lm show` では `severity` に出ます。
- 出てくるコマンド: `lm create --severity <1-4>`, `lm update <id> --severity <1-4> --reason "..."`, `lm show`

### --due
- 一言の定義: Bead の期限（RFC 3339 の時刻）です。期限に価値が依存する作業に付けます。
- 状態遷移や関係: 期限から先行時間（係数の 1 つ）を引いた時点を過ぎた未完了の Bead は、実効優先度が `P1` に上がります。重大度による上限は掛かりません。間に合わない見込みかどうかは Loom が判定せず、急ぐなら `--expedite` を付けます。`lm show` では `due_at` に出ます。
- 出てくるコマンド: `lm create --due <RFC3339>`, `lm update <id> --due <RFC3339>`, `lm update <id> --clear due`

### --expedite
- 一言の定義: 割り込みです。期間と理由を添えて付けると、有効な間その Bead の実効優先度が `P0`（他を止めてでも今やる）になります。
- 状態遷移や関係: 期間と理由（`--reason`）の無い指定は拒否されます。実効優先度はその Bead を `blocks` で塞いでいる前提と、その Bead の子孫にも伝わります。この前提と子孫の集まりを束、割り込みを付ける 1 件を束の根と呼び、根には急ぎたい成果そのものを表す Bead（子を持つなら親の側、前提を持つなら塞がれている下流の側）を選んで、その 1 件にだけ付けます。期限が来れば自動で外れ、未完了のまま切れた Bead には `lm show`・`lm ready` の行に `expedite-expired` の印が出ます。まだ急ぐなら理由を添えて付け直します。同時に付けられる件数と最長期間には上限（係数）があり、超える指定は拒否されます。`lm show` では `expedite_until` と `expedite_reason` に出ます。
- 出てくるコマンド: `lm create --expedite <期間> --reason "..."`, `lm update <id> --expedite <期間> --reason "..."`, `lm update <id> --clear expedite`

### 推論の深さ
- 一言の定義: Bead が求める推論の深さで、1〜5 の整数の順序尺度です。値が大きいほど高い水準の推論を要します。各段の目安は `skills/loom/docs/workflows.md` の `--reasoning-depth` 行にあります。
- 状態遷移や関係: 起票した人やエージェントの自己申告で、未設定を許します。未設定は既定値で埋めず、出力に未設定（`null`）として出ます。`lm ahead --calibration` の差し戻し率は `reasoning_depth` 別に集計されます。`lm show` と `lm export` では `reasoning_depth` に出ます。
- 出てくるコマンド: `lm create --reasoning-depth <1-5>`, `lm update <id> --reasoning-depth <1-5>`, `lm update <id> --clear reasoning_depth`

### redetect_key
- 一言の定義: 同じ事象を繰り返し検知したときに Bead を重複させないための再検知のキー（文字列）です。
- 状態遷移や関係: 同じ名前空間の未完了の Bead の中でキーは一意で、同じキーの検知は新しい Bead を作らずに既存の Bead の `last_redetected_at`（最後に再検知した時刻）を進めます。`last_redetected_at` が再検知の窓（係数 `redetect_window`）の内にある Bead は、[時間調整](#時間調整)で需要ありとして扱われます。
- 設定と参照: 起票するときに `lm create --title "..." --redetect-key <キー>` でキーを付けます。同じ名前空間に同じキーを持つ未完了（`open`・`in_progress`）の Bead があれば、新しい Bead は作られず、その Bead の `last_redetected_at` が今の時刻に進み、出力に `- Redetected: <既存の ID>` が出ます。無ければキーを持つ新しい Bead が作られます。検知のたびに同じキーで `lm create` を打てば、Bead は 1 件のまま `last_redetected_at` だけが進みます。今の値は `lm show <id>` の front matter の `redetect_key`・`last_redetected_at` で読み、`lm export` の JSONL の同じ名前のキーにも出ます（未設定なら `lm show` では `null`）。
- 出てくるコマンド: `lm create --redetect-key <キー>`, `lm show <id>`, `lm export`

### revived_at
- 一言の定義: Bead を `--revive` で手で戻した時刻です。
- 状態遷移や関係: `revived_at` の更新は進捗に数えられるので、[時間調整](#時間調整)で保留になった Bead を、需要を付けずに `lm ready` へ戻します。保留から戻す手は、まず需要を付けることで、`--revive` は需要を付けられないときに使います。
- 設定と参照: `lm update <id> --revive --reason "<戻す理由>"` を打ちます。`revived_at` に今の時刻が刻まれ、理由は履歴に残ります。`--reason` が無いと `lm` は拒否します。今の値は `lm show <id>` の front matter の `revived_at` で読み、`lm export` の JSONL の同じ名前のキーにも出ます（未設定なら `lm show` では `null`）。
- 出てくるコマンド: `lm update <id> --revive --reason "..."`, `lm show <id>`, `lm export`

### 係数
- 一言の定義: 実効優先度の並びや割り込みの上限に使う値の組で、データベースの coefficients 表に保存されます。
- 状態遷移や関係: 期限の先行時間（`due_lead`）と割り込みの同時件数と最長期間（`expedite_max_open`・`expedite_max_age`）は常に使われます。昇格・減衰・保留・取り消し・再検知の窓・重大度ごとの上限と下限のキーは、`time_adjust` が `on` のときの並びと `lm ready --time-adjusted` の出力と `lm hold cancel` に使われます（[時間調整](#時間調整)）。初期値はスキーマの移行が入れ、値域外の値は書き込み時と取り込み時に拒否されます。今の値は `lm coefficient show` が全キーについて値と設定時刻を出し、値は `lm coefficient set <キー> <値> --reason <理由>` で変えます。理由の無い指定・未知のキー・値域外の値は何も書き込まずに拒否され、変更は監査ログに kind=coefficient で残ります。JSONL には `_type=coefficient` の行で入ります。`config.json` の設定（`create.require_milestone`・`compaction.retention_days` など）は係数とは別のもので、置き場所・書式・キーと既定値は [skills/loom/docs/env.md](../skills/loom/docs/env.md)「設定ファイル（`config.json`）」にあります。Loom は値の良し悪しを判断せず、どの値にするかは利用者か実行者が決めます。
- 値と単位: 期間は `72h`・`30m` のような時間の文字列か、日数に `d` を付けた文字列（`7d`）で、正の長さに限ります。段は優先度の段を表す 1〜4 の整数で、件数は 1 以上の整数です。重大度ごとの上限は下限より高い段（小さい数）か同じ段にします。

  | キー | 意味 | 初期値 | 単位 |
  |---|---|---|---|
  | `promote_after` | 昇格の間隔 | `72h` | 期間 |
  | `promote_after_gate` | Gate の昇格の間隔 | `6h` | 期間 |
  | `decay_after` | 減衰の間隔 | `7d` | 期間 |
  | `hold_after` | 保留までの時間 | `14d` | 期間 |
  | `cancel_after` | 取り消しまでの時間 | `30d` | 期間 |
  | `redetect_window` | 再検知の窓 | `7d` | 期間 |
  | `severity_1_ceiling` | 重大度 1 の上限 | `1` | 段 |
  | `severity_2_ceiling`・`severity_2_floor` | 重大度 2 の上限・下限 | `1`・`2` | 段 |
  | `severity_3_ceiling`・`severity_3_floor` | 重大度 3 の上限・下限 | `2`・`4` | 段 |
  | `severity_4_ceiling`・`severity_4_floor` | 重大度 4 の上限・下限 | `3`・`4` | 段 |
  | `due_lead` | 期限の先行時間 | `72h` | 期間 |
  | `expedite_max_open` | 割り込みの同時件数の上限 | `2` | 件数 |
  | `expedite_max_age` | 割り込みの最長期間 | `24h` | 期間 |
  | `cutoff_priority` | 足切りの閾値 | `2` | 段 |
  | `time_adjust` | 時間調整の切り替え | `shadow` | `off`・`shadow`・`on` |

- 設定と参照: 今の値は `lm coefficient show` で読み、1 つのキーは `lm coefficient set <キー> <値> --reason <理由>` で変えます。JSONL で扱うときは、今の値を `lm export` の出力のうち `"_type":"coefficient"` の行で読みます。1 行が 1 つのキーで、`key` がキー、`value` が値、`_set_at.value` が値を決めた時刻です（初期値は `1970-01-01T00:00:00.000Z`）。JSONL で値を変えるときは、変えるキーの行を `{"_set_at":{"value":"2026-10-07T00:00:00.000Z"},"_type":"coefficient","key":"promote_after","value":"48h"}` のように今の値より新しい `_set_at.value` で書いた JSONL ファイルを作り、`lm import <ファイル>` で取り込みます。`_set_at.value` が今の値より古い行は取り込まれません。値域外の値を含むファイルは全体が拒否され、何も変わりません。
- 出てくるコマンド: `lm export`, `lm import <ファイル>`

### 時間調整
- 一言の定義: 経過時間で実効優先度を動かす調整で、需要のある Ready の Bead を昇格させ、需要も進捗も無い Bead を減衰させ、減衰しきったまま時間がたった重大度 3・4 の Bead を保留にします。保留の Bead は `lm ready` に出ません。
- 状態遷移や関係: 需要は、未完了のマイルストーンの子孫である・未完了の下流を `blocks` で塞いでいる・期限を持つ・再検知の窓の内に再検知された、のどれかです。進捗は状態・claim・保存優先度・題・本文・外部参照・依存・期限・割り込み・重大度・`revived_at` の更新で、ラベル・種別・名前空間・推論の深さ・要約の更新と理由だけの記録は数えません。需要か進捗が生じれば、書き込みを待たずに保留から戻ります。保留のまま一定期間たった Bead は `lm hold cancel` で `cancelled` になり、`lm update <id> --reopen` で戻せます。効かせるかどうかは係数 `time_adjust` で決まり、`off` は掛けず、`shadow`（初期値）は既定の並びを変えずに `lm ready --time-adjusted` でだけ結果を見せ、`on` は既定の並びに掛けて `lm hold cancel` に取り消させます。`lm ready` は保留で隠した件数を末尾の `- Held: <件数>` に出し、`lm blocked` は保留の Bead を `← held: no demand since <時刻>` の印つきで並べます。運用は [skills/loom/docs/priority.md](../skills/loom/docs/priority.md)「時間調整と保留」にあります。
- 出てくるコマンド: `lm ready --time-adjusted`, `lm hold cancel --dry-run`, `lm update <id> --revive --reason "..."`, `lm update <id> --reopen`, `lm coefficient show`

### 設定時刻
- 一言の定義: Bead の各項目・依存の `removed`・係数の値を、最後に設定した時刻です。
- 状態遷移や関係: `lm import` が同じ Bead の項目ごとに新しい値を選ぶとき（Last-Writer-Wins）に比べます。データベースでは項目ごとに `<項目>_set_at` の列に入り（`due_at` なら `due_at_set_at`）、`lm export` の JSONL では行の `_set_at` の入れ子に項目名をキーとして入ります。`created_at`・`updated_at` のような出来事の時刻とは別のものです。
- 出てくるコマンド: `lm export`, `lm import <ファイル>`

## 実行者の側

Loom 本体には含まれず、Loom を使う側が用意する担い手と、その担い手が行う作業の語です。

### 実行者
- 一言の定義: cron や CI のスケジュール実行のように、決まった間隔でエージェントを起動し直す仕組みの上で Bead を claim して作業を進めるエージェントです。止まっても次の起動で作業を再開できるので、claim が持ち主の無いまま残りません。
- 自作するときの最小要件: 次の 3 つを満たせば実行者として使えます。(1) cron・launchd・systemd timer・CI のスケジュール実行などで、エージェントを決まった間隔で起動する。(2) 起動ごとに `lm ready --claim` で 1 件取り、作業を終えたら `lm close <id> --summary "..." --reason "..."`、終えられなければ `lm update <id> --release --reason "..."` で手放す。(3) 前の起動が claim したまま止まった Bead を次の起動が見つけ、続きから再開するか解放する（手順は `skills/loom/docs/claim-recovery.md`）。
- 状態遷移や関係: 実行者と対話セッションを併用する運用では、claim は実行者が持ち、止まれば claim を解放して再開します。実行者が 1 つだけの運用（[README](../README.md) の「5 分で使う」のように利用者や 1 つのエージェントが claim から close まで進める場合）では、この区別は要りません。Loom の既定の運用は skill（`skills/loom/`）と [docs/tutorial.md](tutorial.md)「実行者を用意しないとき」に書いてあり、実行者が無くても完結します。実行者は Loom 本体には含まれません。実行者を使うときは、利用者がその実行者に読ませる規約ファイル（リポジトリに置くエージェント向けの規約ファイルなど。名前と置き場所は利用者が決めます）に既定と違う運用を書いて、既定を上書きします。上書きできるのは、(1) 起動のしかた、(2) `wait:` ラベルの拡張種別、(3) 通知の出し方、(4) 受入と merge と close の担い手、(5) PR の題名と本文の検査、(6) 人向け Gate の resolve の担い手、(7) 判定者に委ねる判断の範囲、(8) 着手する Bead の選び方、(9) push と PR 作成の確認、の 9 項目だけです。(6)〜(9) の既定は利用者の承認を残す側にあり、人向け Gate は利用者の指示で開き、受入・出荷・方針は利用者が決め、着手する Bead は利用者が選ぶか確かめ、push と PR の作成は利用者に確認してから行います。書き方は 1 項目 1 行で、項目名と既定と違う振る舞いを書けば足ります。書かなかった項目は既定のとおりに動きます。(1)〜(3) と (5) は実行者の作りに依存するので、書式は実行者ごとに定め、その実行者が実際にする振る舞いを書きます。9 項目すべてを上書きする規約ファイルの記入例を次に示します（コマンド名・ラベル名・パスは例で、実際に用意した実行者のものに置き換えます）。

  ```text
  - 起動のしかた: cron が 10 分ごとに scripts/run-agent.sh を起動し、スクリプトがエージェントを 1 回だけ非対話で走らせる
  - wait: ラベルの拡張種別: wait:deploy-done:<環境名> は、実行者がその環境へのデプロイの成功を確かめたら満たされたとみなす（拡張種別には skills/loom/docs/gate.md の標準の種別と重ならない名前を付ける）
  - 通知の出し方: 人向け Gate を作ったら、実行者が Gate の ID と件名をチャットの運用チャンネルに 1 行で投稿する
  - 受入と merge と close の担い手: 実行者が PR の差分とテスト結果を確かめて merge し、merge 後に `lm close` する
  - PR の題名と本文の検査: PR の題名は「<種別>: <要約>」の形にし、本文に Bead の ID を書く。実行者は push の前にこの 2 つを確かめ、外れていれば直してから PR を作る
  - 人向け Gate の resolve の担い手: 判断材料が揃った人向け Gate は、対話セッションが材料を読んで resolve する
  - 判定者に委ねる判断の範囲: 設計の分岐は判定者が決め、外部への公開と費用の発生する操作だけを利用者に聞く
  - 着手する Bead の選び方: 実行者が `lm ready --claim` で Ready の先頭を順に拾う
  - push と PR 作成の確認: Bead 自身のブランチへの push と PR の作成は、利用者に確認せず行う
  ```

  skill で「実行者の文書」と書いている箇所は、この上書きを書いた規約ファイルを指します。[docs/prd-harness.md](prd-harness.md) が要件を書く製品は実行者の一例で、Loom とは別の製品です。skill や `docs/` の「実行者」はこの製品に限らず、利用者が用意するどの実行者も指します。
- 出てくるコマンド: `lm ready --claim`, `lm update <id> --release`, `lm gate sweep`

### 対話セッション
- 一言の定義: 利用者と対話しながら動くエージェントのセッションで、利用者が閉じれば終わります。Loom 本体には含まれません。
- 状態遷移や関係: 閉じたあとに再開する仕組みが無いので Bead を claim せず、起票・依存の記録・Gate の作成と整理・受入の確認を受け持ちます。着手は実行者が Ready から拾います。
- 出てくるコマンド: `lm create`, `lm dep add`, `lm gate create`, `lm gate resolve`

### 受入
- 一言の定義: 作業の成果（PR の差分・テスト結果）が Bead の求めを満たしているかを確かめ、merge してよいと認めることです。認めた印として PR に付けるラベルを `accepted` と呼びます。受入は Loom 本体には含まれません。
- 状態遷移や関係: Loom は受入を行わず、`accepted` の意味も解釈しません。既定では、利用者か対話セッションが受入をして、merge と `lm close` も行います。実行者の文書は、この担い手を上書きできます。
- 出てくるコマンド: `lm update <id> --external-ref <PR URL>`, `lm close`

### 判定者
- 一言の定義: 設計の分岐や取り消せない操作の可否を決める役割のエージェントです。判定者は Loom 本体には含まれず、Loom は判定者を起動しません。実行者や対話セッションが自分の仕組みで起動します。判定者を置かない運用では、人が `kind:adjudicate` の Gate を読んで `lm gate resolve` か `lm gate reject` で決めます。
- 状態遷移や関係: `kind:adjudicate` の Gate や相談を受け取り、結論と理由を Bead の履歴に残します。Gate を開く・人向け Gate へ格上げするのは、判定者を起動した側が結論に沿って行います。
- 出てくるコマンド: `lm update <id> --reason "..."`, `lm gate create --kind adjudicate`

### 引き継ぎ簿
- 一言の定義: 作業を別のセッションやエージェントへ渡すときに、途中で止まっても続きから再開できるよう、手順と進捗を Bead ごとに記録しておくファイルです。Loom 本体には含まれません。
- 状態遷移や関係: 任意の運用で、Loom は必須にしません。書く内容（手順・済んだ手順の印・成果物のパス・次にやること）と置き場所は実行者が決めます。Bead から辿れるようにする場合は `lm update <id> --external-ref <パス>` で所在を張り、相対パスは `$LM_DIR` を基準に読みます。Loom は中身を解釈せず、Bead の状態や Ready の判定にも影響しません。
- 出てくるコマンド: `lm update <id> --external-ref <パス>`

### watcher
- 一言の定義: Loom の外で条件を監視し、満たされたら Gate を解除（`resolve`）する外部プロセスのことです。Loom 本体には含まれません。
- 状態遷移や関係: `kind:external` の Gate や外部 PR の merge 待ち（`wait:pr-merged:`）など、Loom の外で条件が満たされたことを検知して `lm gate resolve --reason "<検知した条件>"` を実行します。監視する条件やプロセスの起動・維持は実行者が用意します。
- 出てくるコマンド: `lm gate resolve --reason "..."`

### probe
- 一言の定義: 待ち条件や受入基準が満たされているかを、実行者がコードで自動計測・検査する仕組みです。Loom 本体には含まれません。
- 状態遷移や関係: Gate の待ち条件（`wait:` ラベル）やマイルストーンの完了条件、成果物の検査を実行者が probe で測り、合格すれば Gate の resolve や PR の受入へ進めます。Loom は判定を行いません。実行者が probe で測った結果を、`lm gate resolve` などのコマンドで Loom に書き戻します。
- 出てくるコマンド: `lm update <id> --add-label wait:...`（probe が測る待ち条件を Gate に付ける）, `lm gate resolve`（probe の結果を書き戻す）。probe 自体は Loom のコマンドではありません。

### statusline
- 一言の定義: 実行者やエージェント CLI が端末下部に出す状態表示のことです。Loom 本体には含まれません。
- 状態遷移や関係: Gate の件数（人の承認を待つ「Needs you」節（[Gate の節](#gate-の節)）の件数など）や現在の進行状況を表示するために利用されます。Loom は表示の機能を持たず、元となる `lm gate` などのコマンド出力を提供するのみです。
- 出てくるコマンド: `lm gate`

