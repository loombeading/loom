## 定型の作業を起票する（ワークフロー手順）

定型の Bead 群は、以下の手順どおりに `lm create` と `lm gate create` を順に実行して起票する。Loom はテンプレート機構を持たない。各コマンドの `Created:` 行の ID を次のコマンドの `--parent` / `--blocked-by` / `--blocks` に渡す。

手順の番号付きの 1 行（1 コマンド）を Step と呼び、Step 1 つが Bead か Gate を 1 件起票する。手順に出てくる `lm create` のフラグは次の意味である（`lm help create` にも同じ説明がある）。

| フラグ | 意味 |
|---|---|
| `--parent <ID>` | 起票する Bead を `<ID>` の子にする（`parent-child` リンク） |
| `--blocked-by <ID>` | 起票と同時に、`<ID>` が閉じるまで起票する Bead を塞ぐ（`blocks` リンク）。繰り返し指定できる。起票済みの Bead 同士に後から張る `lm dep add <起票した ID> <ID> --type blocks` と同じリンクになる |
| `--reasoning-depth <1-5>` | 作業に要る推論の深さの見積もり。1 = 1 回で済む抽出・定型の作業、2 = 前提を実物で確かめてから既知の手順を当てる、3 = 1 つの論点の中で選択肢から選ぶ、4 = 論点をまたぐ複数段の推論（仕様を固めてから実装する）、5 = 自己修正を含む自律的な反復。実行者がモデルや分け方を選ぶ材料に読む |

起票し終えたら、ID の取り違え・Step 飛ばし・部分失敗で欠けたグラフが無いかを次の 2 つで確かめる。確認はこの 2 つで完結する。

- `lm show <EPIC>` の `## Dependencies` に、手順で `--parent <EPIC>` を付けた Bead がすべて子として並んでいること。
- `lm blocked` に、手順で `--blocked-by` を付けた Bead と Gate で塞いだ Bead が、手順に書いた前提に塞がれて並んでいること。

欠けていれば、欠けた Step を同じフラグで起票し直すか、`lm dep add` で張る。違反の直し方（後続を cancelled にする・前提を張り替える・リリース Epic へ `--parent` する・どのリリースにも属さない理由を `--reason` に残す）は人かエージェントが判断する。

### epic-phase

Epic 1件分の定型フェーズを起票する。実装タスク1〜3（順に依存）→ 受け入れテスト（PR がすべて merge されたら開く Gate 付き）→ テンプレート更新 の 5 件の Bead からなる（Epic と Gate を入れると Step は 7 つ）。手順の中の `<name>`・`<spec_ref>`・`<resolver>` は下の変数表の値に置き換える。`spec_ref` は実装の拠り所にする仕様の参照先である。Epic のタイトル `epic-phase (name=<name>, spec_ref=<spec_ref>)` は、どの手順をどの変数で起票したかを `lm list` や `lm search` の一覧から読めるように残す書式で、`lm` はこの書式を解釈しない。受け入れテストは、実装タスクの成果が `spec_ref` の仕様を満たすことを確かめる Bead である。テンプレート更新は、その変更を受けて、利用者のリポジトリにある雛形（起票の手順書・文書やコードの雛形など、次の同種の作業が写して使うもの）を直す Bead である。直す雛形が無ければ理由を `--reason` に書いて閉じる。

変数:

| 名前 | 説明 |
|---|---|
| name | Epic の名前（起票する機能・変更の名称） |
| spec_ref | 仕様の参照先（仕様書・設計書の節番号や Bead ID など） |
| resolver | PR がすべて merge されたことを検知して resolve する側（watcher プロセスかセッション名） |

手順:

1. `lm create --title "epic-phase (name=<name>, spec_ref=<spec_ref>)"` → `<EPIC>`
2. `lm create --title "<name>: 実装タスク1（<spec_ref>）" --description "<spec_ref> に基づく実装タスク1。" --parent <EPIC>` → `<IMPL1>`
3. `lm create --title "<name>: 実装タスク2（<spec_ref>）" --description "<spec_ref> に基づく実装タスク2。" --parent <EPIC> --blocked-by <IMPL1>` → `<IMPL2>`
4. `lm create --title "<name>: 実装タスク3（<spec_ref>）" --description "<spec_ref> に基づく実装タスク3。" --parent <EPIC> --blocked-by <IMPL2>` → `<IMPL3>`
5. `lm create --title "<name>: 受け入れテスト" --description "<spec_ref> に対する受け入れテストを実施する。" --parent <EPIC> --blocked-by <IMPL3>` → `<ACCEPT>`
6. `lm gate create --kind external --blocks <ACCEPT> --subject "PR がすべて merge されたら開く" --resolver "<resolver>"`
7. `lm create --title "<name>: テンプレート更新" --description "<name> の変更を反映してテンプレートを更新する。" --parent <EPIC> --blocked-by <ACCEPT>`

### proposal-multi

複数ステップの提案を Gate 付きで一括起票する。1 ステップの提案は `lm gate create --kind adjudicate --subject "<件名>" --current "…" --proposal "…" --check "…" --blocks <id>`（SKILL.md）を使う。提案が複数ステップ（調査→実装→文書反映）に分かれる場合はこの手順を使う。採用の判断は調査の Bead を塞ぐ Gate 1 回のみで行い、採用後の実装と文書反映の Bead には Gate を付けない。

変数:

| 名前 | 説明 |
|---|---|
| subject | 提案の件名 |
| current | 現状 |
| proposal | 提案の内容 |
| check | チェックすると何が始まるか |

手順:

1. `lm create --title "epic"` → `<EPIC>`（Epic が無ければ先に起票する）
2. `lm create --title "<subject>: 調査" --description "現状: <current>。提案: <proposal>" --parent <EPIC>` → `<INVESTIGATE>`
3. `lm gate create --kind adjudicate --blocks <INVESTIGATE> --subject "<subject>" --current "<current>" --proposal "<proposal>" --check "<check>"`
4. `lm create --title "<subject>: 実装" --description "<subject> の実装。現状: <current>。提案: <proposal>" --parent <EPIC> --blocked-by <INVESTIGATE>` → `<IMPLEMENT>`
5. `lm create --title "<subject>: 文書反映（仕様書・設計書・チュートリアル・変更履歴）" --description "<subject> の実装内容を仕様書・設計書・チュートリアル・変更履歴へ反映する。" --parent <EPIC> --blocked-by <IMPLEMENT>`
