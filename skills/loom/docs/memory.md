## 要約（`--summary`）の入力

- Bead が長い作業ログや議論を経てクローズされる場合、クローズ前に`lm update <id> --summary "<要約>" --reason "..."` で要約を残しておくことを推奨する（`lm close <id> --summary "<要約>" --reason "..."` で閉じるのと同時に残せる）。
- 要約は、後述の Compaction（「Compaction（記憶の減衰）の読み方」）で本文の代わりに表示される、実質的な「圧縮後の記憶」になる。**削減の対象になるのは要約を持つ Bead だけ**——要約を残さずにクローズすると Compaction は起きず、本文がそのまま表示され続ける（削減すると本文の代わりに示すものが無くなるためである）。
- 要約を消すときは、`--summary ""`（空文字）ではなく `lm update <id> --clear summary` を使う（空文字は変えない扱いになる）。

## Compaction（記憶の減衰）の読み方

- `lm show <id>` は、対象 Bead が終端状態（`closed`/`cancelled`）かつ要約を持ち、`closed_at` が保持期間（既定 0 日、`config.json` の`compaction.retention_days` で延ばせる。置き場所と書式は docs/env.md「設定ファイル（`config.json`）」）より前であれば、本文（Description）を省略し、代わりに要約を表示し、続けて固定の通知行`Compacted: description omitted
  (closed more than retention_days=<N> days ago); use \`lm export\` or
  \`lm show --full\` to see the full record` を返す。要約を持たない Bead は、どれだけ時間が経っても削減されず、本文がそのまま表示され続ける。
- **全項目（省略前の完全な記録）を得る方法は 2 つ**: (1) `lm export` で決定的直列化された JSONL を取得する（Compaction が変えるのは `lm show` の表示だけで、保存データ自体はそのまま残る）。(2) `lm show <id> --full` で投影を掛けずに本文を出す（`lm show <id> --all-deps` が既定では隠す、`lm dep remove` で取り除いた（`removed`）リンクも `## Dependencies` に出すのと同種の表示オプション。「`LM_NOW`（時刻の固定）」のとおり `LM_NOW` は試験用ビルドだけが読む時刻注入で、利用者が Compaction を回避するときは (1) か (2) を使う）。
- Compaction が影響するのは `lm show` の単体表示だけで、列挙系コマンド（`lm list` 等）の項目構造はそのまま保たれる。

## `LM_NOW`（時刻の固定）

- `LM_NOW`（RFC 3339）は**試験用ビルドだけが読む時刻注入**で、利用者向けの手段ではなく試験のためにある。ビルドタグ `lmtestclock` を付けてビルドした `lm` だけが、現在時刻を使う判定（Compaction・claim の期限と引き取り・実効優先度・Gate の `wait:after`／`wait:date` など）でその値を使う。リリースのバイナリは `LM_NOW` を読まずに実時刻を使い、設定されていれば stderr に `warning: LM_NOW is ignored by release builds` を 1 行出して処理を続ける。Compaction を回避して全文を見たいだけなら、`lm show <id> --full`（「Compaction（記憶の減衰）の読み方」）または `lm export` を使う。
- 試験用ビルドに不正な値を与えると、時刻を使うコマンドは Exit Code 1 の固定行エラーで止まる。
- `LM_NOW` が使われるのは判定だけで、`created_at` 等のレコードに実際に書き込まれる時刻は実時刻のままである。
- `lm list`・`lm ready`・`lm search` の `--updated-before <日時>` は `updated_at` がその日時より前の Bead だけに、`--updated-after <日時>` はその日時より後の Bead だけに絞り込む（どちらも境界の日時ちょうどの Bead は対象外になる。両方を付けると間の期間になる。`--updated-before` の用例は docs/claim-recovery.md）。日時は RFC3339 か `YYYY-MM-DD` の絶対時刻だけを受け取り、相対指定は受け付けず、これらの値自体は `lm` が `LM_NOW` から自動導出するものではないため、`LM_NOW` を使う場合は呼び出し側（エージェント・シェル）が同じ基準時刻から両方の値を作って渡す。そうすることで `lm show`の Compaction 判定と `lm list --updated-before` 等の絞り込みが同じ「現在」を基準にでき、両者の判定が揃う。
