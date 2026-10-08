# 用語集

Loom で用いる中核概念と用語について、その定義、状態遷移や関係、対応するコマンドをまとめています。用語は、Loom 本体が保存して扱う語と、Loom を使う実行者の側の語に分けて載せています。

語の定義はこのページが正です。`--severity` のようなフラグ名の見出しへは、`glossary.md#--severity` のように先頭の `--` を含めた形でリンクできます。

## Loom 本体

`lm` が保存し、コマンドの出力に現れる語です。

### Bead
- 一言の定義: Loom で作業・タスク・Gate・Epic を管理するときの最小単位です。
- 状態遷移や関係: `open`（未着手）から `in_progress`（着手）を経て `closed`（完了）へ進みます。やらないと判断したものは `cancelled`（取りやめ）にします。`closed` と `cancelled` が終端状態で、依存関係（DAG）によってブロックされることがあります。
- 出てくるコマンド: `lm create`, `lm show`, `lm list`, `lm close`

### 依存
- 一言の定義: Bead 同士の先行・後行の関係（DAG）を表す有向の辺です。
- 状態遷移や関係: `blocks`（先行 Bead が閉じるまで後行 Bead は Ready にならない）・`parent-child`（親子関係）・`discovered-from`（作業中に見つけた派生課題の出どころ）・`duplicates`（重複）の 4 種があります。このうち Ready を左右するのは `blocks` だけで、循環する依存は拒否されます。
- 出てくるコマンド: `lm dep add`, `lm dep remove`

### 監査ログ
- 一言の定義: Bead の状態や項目を変えた操作を、操作した actor・時刻・理由（`--reason`）とともに 1 件ずつ残した履歴で、データベースの audit_log 表に保存されます。
- 状態遷移や関係: 記録は追記だけで、項目の変更・依存の追加と除去・claim と解放などを残します。理由だけを残したいときは、ほかのフラグを付けずに `lm update <id> --reason "..."` を打ちます。`lm import` で取り込んだ記録は、手元で書いた記録と区別して残ります。`lm show` の `## History` に出て、全文は `--full` で読めます。`lm export` の JSONL には含まれません。
- 出てくるコマンド: `lm show <id>`, `lm show <id> --full`, `lm update <id> --reason "..."`

### 実効優先度
- 一言の定義: 保存した優先度に、割り込み・期限・前提や親子の関係による引き上げを重ねて導いた、並びに使う優先度です。
- 状態遷移や関係: 割り込みを持つ Bead は `P0`、期限の手前に入った Bead は `P1` になり、引き上げは塞いでいる前提と子にも伝わります。`lm ready`・`lm blocked`・`lm gate`・`lm list --sort priority` はこの値で並び、保存値と違う行の末尾に `eff=P<n>(<理由>)` が付きます。`lm show` では `effective_priority` と `effective_priority_source` に出ます。
- 出てくるコマンド: `lm ready`, `lm list --sort priority`, `lm show`, `lm dep add --dry-run`

### Ready
- 一言の定義: 未着手（`open`）でブロッカーが無く、今すぐ着手できる Bead の状態です。
- 状態遷移や関係: 先行するブロッカーが `lm close` や Gate の resolve で解消されると、それに依存していた後続の Bead が Newly ready になります。
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
- 状態遷移や関係: `open` から `in_progress` へ進みます。完了したときは `lm close` で閉じ、中断して手放すときは `--release` で `open` に戻します。claim した actor は `claimed_by` に、claim の期限は `claim_expires_at` に残ります。期限は `--claim-ttl` で決め、期限が切れた claim は別の actor が引き継げます（引き継ぎは監査ログに `claim_takeover` として残ります）。
- 出てくるコマンド: `lm ready --claim`, `lm update <id> --claim`, `lm update <id> --release`, `lm list --claim-expired`

### Gate
- 一言の定義: 自動の進行を止め、人間の承認や外部条件の成立を待つための特殊な Bead です。
- 状態遷移や関係: `open`（判断待ち・条件待ち）の Gate は、`resolve`（合流・許可）で後続を unblock するか、`reject`（取り下げ）で後続ごと破棄します。待ち条件は `kind`（human / confirm / adjudicate / external）や `wait:` ラベル（外部 PR の merge など）で宣言します。
- 出てくるコマンド: `lm gate`, `lm gate create`, `lm gate resolve`, `lm gate reject`, `lm gate sweep`

### Gate の持ち主
- 一言の定義: その Gate を作った actor です。Gate の履歴（`lm show <id>` の `## History`）の `created` の行に記録された actor がそのまま持ち主になります。
- 状態遷移や関係: actor は `--actor` か環境変数 `LM_ACTOR` で決まり、どちらも無いときは実行環境が決める既定値になります。持ち主を表す別の印は要りません。
- 出てくるコマンド: `lm gate create`, `lm show <id>`

### 孤児 Gate
- 一言の定義: 持ち主のセッションが終わったまま `open` に残った `kind:external` の Gate です。
- 状態遷移や関係: 誰も resolve しないので、放っておくと下流の Bead を塞ぎ続けます。実行者は持ち主の actor が今も動いているかを測り、止まっていると確かめられたときだけ `lm gate resolve` し、確かめられないときは根本原因の Bead を起票します。何をもって「動いている」とするか（プロセスの生存・監査ログへの直近の書き込みの時刻・claim の期限など）は Loom が定めず、利用者が決めます。
- 出てくるコマンド: `lm gate`, `lm gate resolve`, `lm show <id> --full`

### Epic
- 一言の定義: 複数の子タスク（Bead）を束ねる、大きめの親タスクの単位です。
- 状態遷移や関係: 子の Bead とは `parent-child` の依存で結ばれます。`milestone:<名前>` ラベルを付けると、マイルストーンとして扱えます。
- 出てくるコマンド: `lm create --parent <Epic ID>`, `lm list --parent <Epic ID>`, `lm ahead`

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
- 状態遷移や関係: 表示上の投影なので、保存データは書き換えません。本文の代わりに要約（`--summary`）が出ますが、全文は `--full` で読めます。
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
- 状態遷移や関係: 起票時に証拠を添えて付け、迷えば上（小さい値）を選びます。変えるときは `--reason` に実測を書きます。重大度は保存して表示するだけで、今の `lm` は並び（実効優先度）にも `lm ready` に出るかどうかにも使いません。`lm show` では `severity` に出ます。
- 出てくるコマンド: `lm create --severity <1-4>`, `lm update <id> --severity <1-4> --reason "..."`, `lm show`

### --due
- 一言の定義: Bead の期限（RFC 3339 の時刻）です。期限に価値が依存する作業に付けます。
- 状態遷移や関係: 期限から先行時間（係数の 1 つ）を引いた時点を過ぎた未完了の Bead は、実効優先度が `P1` に上がります。重大度による上限は掛かりません。間に合わない見込みかどうかは Loom が判定せず、急ぐなら `--expedite` を付けます。`lm show` では `due_at` に出ます。
- 出てくるコマンド: `lm create --due <RFC3339>`, `lm update <id> --due <RFC3339>`, `lm update <id> --clear due`

### --expedite
- 一言の定義: 割り込みです。期間と理由を添えて付けると、有効な間その Bead の実効優先度が `P0`（他を止めてでも今やる）になります。
- 状態遷移や関係: 期間と理由（`--reason`）の無い指定は拒否されます。実効優先度は塞いでいる前提にも伝わるので、束の根の 1 件にだけ付けます。期限が来れば自動で外れ、未完了のまま切れた Bead には `lm show`・`lm ready` の行に印が出ます。まだ急ぐなら理由を添えて付け直します。同時に付けられる件数と最長期間には上限（係数）があり、超える指定は拒否されます。`lm show` では `expedite_until` と `expedite_reason` に出ます。
- 出てくるコマンド: `lm create --expedite <期間> --reason "..."`, `lm update <id> --expedite <期間> --reason "..."`, `lm update <id> --clear expedite`

### 推論の深さ
- 一言の定義: Bead が求める推論の深さで、1〜5 の整数の順序尺度です。値が大きいほど高い水準の推論を要します。
- 状態遷移や関係: 起票した人やエージェントの自己申告で、未設定を許します。未設定は既定値で埋めず、出力に未設定（`null`）として出ます。`lm ahead --calibration` の差し戻し率は `reasoning_depth` 別に集計されます。`lm show` と `lm export` では `reasoning_depth` に出ます。
- 出てくるコマンド: `lm create --reasoning-depth <1-5>`, `lm update <id> --reasoning-depth <1-5>`, `lm update <id> --clear reasoning_depth`

### redetect_key
- 一言の定義: 同じ事象を繰り返し検知したときに Bead を重複させないための再検知のキー（文字列）です。
- 状態遷移や関係: 同じ名前空間の未完了の Bead の中でキーは一意で、同じキーの検知は新しい Bead を作らずに既存の Bead の `last_redetected_at`（最後に再検知した時刻）を進めます。今の `lm` は `last_redetected_at` を保存して表示するだけで、並び（実効優先度）には使いません。
- 設定と参照: 起票するときに `lm create --title "..." --redetect-key <キー>` でキーを付けます。同じ名前空間に同じキーを持つ未完了（`open`・`in_progress`）の Bead があれば、新しい Bead は作られず、その Bead の `last_redetected_at` が今の時刻に進み、出力に `- Redetected: <既存の ID>` が出ます。無ければキーを持つ新しい Bead が作られます。検知のたびに同じキーで `lm create` を打てば、Bead は 1 件のまま `last_redetected_at` だけが進みます。今の値は `lm show <id>` の front matter の `redetect_key`・`last_redetected_at` で読み、`lm export` の JSONL の同じ名前のキーにも出ます（未設定なら `lm show` では `null`）。
- 出てくるコマンド: `lm create --redetect-key <キー>`, `lm show <id>`, `lm export`

### revived_at
- 一言の定義: Bead を `--revive` で手で戻した時刻です。
- 状態遷移や関係: 今の `lm` は Bead を保留にしないので、`revived_at` は時刻と理由を記録するだけで、並び（実効優先度）にも `lm ready` に出るかどうかにも影響しません。
- 設定と参照: `lm update <id> --revive --reason "<戻す理由>"` を打ちます。`revived_at` に今の時刻が刻まれ、理由は履歴に残ります。`--reason` が無いと `lm` は拒否します。今の値は `lm show <id>` の front matter の `revived_at` で読み、`lm export` の JSONL の同じ名前のキーにも出ます（未設定なら `lm show` では `null`）。
- 出てくるコマンド: `lm update <id> --revive --reason "..."`, `lm show <id>`, `lm export`

### 係数
- 一言の定義: 実効優先度の並びや割り込みの上限に使う値の組で、データベースの coefficients 表に保存されます。
- 状態遷移や関係: 今の `lm` が読むのは期限の先行時間（`due_lead`）と割り込みの同時件数と最長期間（`expedite_max_open`・`expedite_max_age`）だけです。ほかのキー（`promote_after`・`promote_after_gate`・`decay_after`・`hold_after`・`cancel_after`・`redetect_window`・`severity_<n>_ceiling`・`severity_<n>_floor`・`cutoff_priority`）は初期値として保存されるだけで、動作には使われません。初期値はスキーマの移行が入れ、値域外の値は取り込み時に拒否されます。係数を表示・変更するコマンドは無く、JSONL には `_type=coefficient` の行で入ります。Loom は値の良し悪しを判断せず、どの値にするかは利用者か実行者が決めます。
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

- 設定と参照: 今の値は `lm export` の出力のうち `"_type":"coefficient"` の行で読みます。1 行が 1 つのキーで、`key` がキー、`value` が値、`_set_at.value` が値を決めた時刻です（初期値は `1970-01-01T00:00:00.000Z`）。値を変えるときは、変えるキーの行を `{"_set_at":{"value":"2026-10-07T00:00:00.000Z"},"_type":"coefficient","key":"promote_after","value":"48h"}` のように今の値より新しい `_set_at.value` で書いた JSONL ファイルを作り、`lm import <ファイル>` で取り込みます。`_set_at.value` が今の値より古い行は取り込まれません。値域外の値を含むファイルは全体が拒否され、何も変わりません。
- 出てくるコマンド: `lm export`, `lm import <ファイル>`

### 設定時刻
- 一言の定義: Bead の各項目・依存の `removed`・係数の値を、最後に設定した時刻です。
- 状態遷移や関係: `lm import` が同じ Bead の項目ごとに新しい値を選ぶとき（Last-Writer-Wins）に比べます。データベースでは項目ごとに `<項目>_set_at` の列に入り（`due_at` なら `due_at_set_at`）、`lm export` の JSONL では行の `_set_at` の入れ子に項目名をキーとして入ります。`created_at`・`updated_at` のような出来事の時刻とは別のものです。
- 出てくるコマンド: `lm export`, `lm import <ファイル>`

## 実行者の側

Loom 本体には含まれず、Loom を使う側が用意する担い手と、その担い手が行う作業の語です。

### 実行者
- 一言の定義: cron や CI のスケジュール実行のように、決まった間隔でエージェントを起動し直す仕組みの上で Bead を claim して作業を進めるエージェントです。止まっても次の起動で作業を再開できるので、claim が持ち主の無いまま残りません。
- 自作するときの最小要件: 次の 3 つを満たせば実行者として使えます。(1) cron・launchd・systemd timer・CI のスケジュール実行などで、エージェントを決まった間隔で起動する。(2) 起動ごとに `lm ready --claim` で 1 件取り、作業を終えたら `lm close <id> --summary "..." --reason "..."`、終えられなければ `lm update <id> --release --reason "..."` で手放す。(3) 前の起動が claim したまま止まった Bead を次の起動が見つけ、続きから再開するか解放する（手順は `skills/loom/docs/claim-recovery.md`）。
- 状態遷移や関係: claim を持てるのは実行者だけで、止まれば claim を解放して再開します。Loom の既定の運用は skill（`skills/loom/`）と [docs/tutorial.md](tutorial.md)「実行者を用意しないとき」に書いてあり、実行者が無くても完結します。実行者は Loom 本体には含まれません。実行者を使うときは、利用者がその実行者に読ませる規約ファイル（リポジトリに置くエージェント向けの規約ファイルなど。名前と置き場所は利用者が決めます）に既定と違う運用を書いて、既定を上書きします。上書きできるのは、(1) 起動のしかた、(2) `wait:` ラベルの拡張種別、(3) 通知の出し方、(4) 受入と merge と close の担い手、(5) PR の題名と本文の検査、の 5 項目だけです。書き方は 1 項目 1 行で、項目名と既定と違う振る舞いを書けば足ります（例: 「受入と merge と close の担い手: 実行者が PR の差分とテスト結果を確かめて merge し、merge 後に `lm close` する」）。書かなかった項目は既定のとおりに動きます。skill で「実行者の文書」と書いている箇所は、この上書きを書いた規約ファイルを指します。[docs/prd-harness.md](prd-harness.md) が要件を書く製品は実行者の一例で、Loom とは別の製品です。skill や `docs/` の「実行者」はこの製品に限らず、利用者が用意するどの実行者も指します。
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
- 状態遷移や関係: Gate の待ち条件（`wait:` ラベル）やマイルストーンの完了条件、成果物の検査を実行者が probe で測り、合格すれば Gate の resolve や PR の受入へ進めます。Loom は判定を行わず、probe の結果を受けた実行者側のコマンド操作として反映されます。
- 出てくるコマンド: `lm gate resolve`, `lm update <id> --add-label wait:...`

### statusline
- 一言の定義: 実行者やエージェント CLI が端末下部に出す状態表示のことです。Loom 本体には含まれません。
- 状態遷移や関係: Gate の件数（人の承認を待つ「Needs you」節の件数など）や現在の進行状況を表示するために利用されます。Loom は表示の機能を持たず、元となる `lm gate` などのコマンド出力を提供するのみです。
- 出てくるコマンド: `lm gate`

