## 複数ワークストリーム（`LM_NAMESPACE`）

- 同じデータベースを複数のワークストリーム（プロジェクト・チーム・エージェント種別など）で分けて使いたい場合は、名前空間を使う。
- `LM_NAMESPACE` 環境変数を設定すると、`lm create` で新規に切る Bead の既定の名前空間と、`lm list` / `lm ready` / `lm blocked` / `lm search` の既定の絞り込み名前空間の両方に効く。個別コマンドの `--namespace` フラグはこれを上書きする。
- 名前空間ごとの件数は `lm list` で数える。`lm list --namespace <名前空間> --all --limit 1` の `Truncated: showing 1 of <N>` の `<N>` が全件数、`--all` を外すと未完了の件数になる。
- データベースにある名前空間を列挙するときは、`lm export` の出力を `jq -r 'select(.namespace != null) | [.namespace, .status] | @tsv' | sort | uniq -c` に渡し、名前空間と状態ごとの件数を出す。

## 環境変数

| 変数 | 用途 |
| --- | --- |
| `LM_DIR` | データディレクトリ（`loom.db` を置く場所。リポジトリごとに分けたいときは `<リポジトリ>/.loom` を明示する例が多いが、既定は下記の XDG のデータ置き場である）。**絶対パス**で指定する（相対パスはエラー）。未設定なら `$XDG_DATA_HOME/loom`（`XDG_DATA_HOME` が未設定か相対パスなら `~/.local/share/loom`）を使う（`LM_DIR` > XDG のデータ置き場 > エラー）。探すのはこの 2 つだけで、カレントディレクトリの `./.loom` と親方向は探索の対象外である。解決先は `lm where` で確かめる。worktree・サブディレクトリ・別プロセスで動くエージェントから使う場合は必ず明示する。 |
| `LM_NAMESPACE` | 既定の名前空間（「複数ワークストリーム（`LM_NAMESPACE`）」）。 |
| `LM_ACTOR` | `--actor` フラグが無いときに監査ログへ記録する主体名。 |
| `NO_COLOR` | 設定されていれば装飾（色）を無効にする。`LM_COLOR=always` より優先する。 |
| `LM_COLOR` | `always` を設定すると非 TTY でも装飾を強制する。エージェントは非 TTY で動くため、既定では装飾無しで出力する。パイプ越しに色が欲しい場合に使う。 |
| `LM_NOW` | 試験用ビルド（ビルドタグ `lmtestclock`）だけが読む時刻注入（RFC 3339）で、利用者向けの手段ではなく試験のためにある。リリースのバイナリは読まずに実時刻を使い、設定されていれば stderr に警告を 1 行出す（docs/memory.md「`LM_NOW`（時刻の固定）」）。Compaction を回避して全文を見たいだけなら `lm show <id> --full` または `lm export` を使う（docs/memory.md）。 |

## 設定ファイル（`config.json`）

- 置き場所は `$XDG_CONFIG_HOME/loom/config.json`（`XDG_CONFIG_HOME` が未設定か相対パスなら `~/.config/loom/config.json`）で、`LM_DIR` とは独立に決まる。`$LM_DIR/config.json` やリポジトリ内の `config.json` は読み込みの対象外である。プロジェクトごとに設定を変えたいときは `XDG_CONFIG_HOME` を上書きする。解決先と有無は `lm where` の `Config:` 行で確かめる（無ければ `(not found)` が付く）。
- ファイルが無ければすべて既定値で動く。`lm` は設定を書き換えるコマンドを持たないので、利用者がファイルを直接編集する。`lm` はコマンドを起動するたびにファイルを読むので、編集は次に打つコマンドから効く。
- 書式は JSON のオブジェクトで、節ごとにキーを入れ子にする。知らない節・キーはエラーになる（綴りの誤りを黙って無視せずに知らせる）。各節と最上位に注記用の `note`（文字列）を置ける。
- 優先順位はコマンドラインのフラグ > 環境変数 > `config.json` > 既定値である。

```json
{
  "compaction": {"retention_days": 7},
  "create": {"require_milestone": true}
}
```

| キー | 既定値 | 意味 |
| --- | --- | --- |
| `compaction.retention_days` | `0` | 閉じた Bead の本文を要約に置き換えるまでの保持日数（docs/memory.md「Compaction（記憶の減衰）の読み方」） |
| `list.limit` | `50` | 列挙・検索コマンドの出力件数の上限。`--limit` が優先する |
| `namespace.default` | （なし） | 既定の名前空間。`LM_NAMESPACE` と `--namespace` が優先する |
| `db.busy_timeout_ms` | `5000` | 書き込みロックを待つ上限（ミリ秒。docs/concurrency.md） |
| `create.require_milestone` | `false` | 真なら、`--parent` も `--discovered-from` も `--no-milestone` も無い `lm create`（Gate を除く）を拒否する（docs/milestone.md「起票と並び」） |
| `id.short_len` | `4` | 新しく切る Bead の短縮 ID の桁数（下限 3） |

## worktree・別プロセスでの注意

- 作業ディレクトリがリポジトリのルートと異なる場所（git worktree・別プロセスで動くエージェント等）では、必ず `LM_DIR=<絶対パス>/.loom` を明示的に渡す（相対パスではなく絶対パスで渡す）。
- 信頼度の低いワーカーに `lm` を渡すときは、DB ディレクトリ（`LM_DIR`）を読み取り専用でマウントするか、DB に書き込み権限の無い利用者でワーカーを動かす。`lm` は DB ファイルか DB のディレクトリに書けないと読み取り専用モードで開き、すべての書き込み（Bead・リンク・監査ログ・設定を含む）を Exit Code 1 で拒否して標準エラーに拒否の事実を出し、参照系コマンドは通常どおり動かす。どちらで開いたかは `lm where` の `Readonly:` 行で確かめる。読み取り専用モードを有効にする環境変数やフラグは無く、境界は OS のマウントと権限が持つ。
- 引き継ぎ簿は任意の運用で、使うかどうかは実行者が決める。作業を別のエージェントへ渡すときに、途中で止まっても続きから再開できるよう、手順と進捗を Bead ごとのファイルに書いておく使い方である。書く内容（手順の一覧・済んだ手順の印・作った成果物のパス・次にやること）と置き場所は、実行者が決める。
- 引き継ぎ簿を Bead から辿れるようにするなら、`lm update <id> --external-ref <パス>` で所在を張る。相対パスは `$LM_DIR` を基準に読む約束である。`lm` は所在を `lm show` の `external_refs` に出すだけで、中身の解釈と、Bead の状態・Ready の判定への反映は対象外とする。

## `lm` の版を上げた後

- `lm` の版を上げたら、使う前に `lm migrate` を実行する。新しい版が期待するスキーマ版より DB が古いと、開いた時点で拒否される。DB の版は `lm where` の `Schema:` 行で確かめる。

