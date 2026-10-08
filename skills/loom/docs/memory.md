## 要約（`--summary`）の入力

- Bead が長い作業ログや議論を経てクローズされる場合、クローズ前に`lm update <id> --summary "<要約>" --reason "..."` で要約を残しておくことを推奨する（`lm close <id> --summary "<要約>" --reason "..."` で閉じるのと同時に残せる）。
- 要約は、後述の Compaction（「Compaction（記憶の減衰）の読み方」）で本文の代わりに表示される、実質的な「圧縮後の記憶」になる。**削減の対象になるのは要約を持つ Bead だけ**——要約を残さずにクローズすると Compaction は起きず、本文がそのまま表示され続ける（削減すると本文の代わりに示すものが無くなるためである）。
- `--summary ""`（空文字）を指定すると要約を消去できる。

## Compaction（記憶の減衰）の読み方

- `lm show <id>` は、対象 Bead が終端状態（`closed`/`cancelled`）かつ要約を持ち、`closed_at` が保持期間（既定 0 日、`config.json` の`compaction.retention_days` で延ばせる）より前であれば、本文（Description）を省略し、代わりに要約を表示し、続けて固定の通知行`Compacted: description omitted
  (closed more than retention_days=<N> days ago); use \`lm export\` or
  \`lm show --full\` to see the full record` を返す。要約を持たない Bead は、どれだけ時間が経っても削減されず、本文がそのまま表示され続ける。
- **全項目（省略前の完全な記録）を得る方法は 2 つ**: (1) `lm export` で決定的直列化された JSONL を取得する（Compaction は `lm show` の表示にのみ影響し、保存データ自体は消えない）。(2) `lm show <id> --full` で投影を掛けずに本文を出す（`--all-deps` と同種の表示オプション。`LM_NOW`を巻き戻す遠回しな手段は使わない——「`LM_NOW`（時刻の固定）」のとおり `LM_NOW` は受け入れ試験・診断のための時刻注入であり、利用者が Compaction を回避する手段ではない）。
- Compaction は列挙系コマンド（`lm list` 等）の項目構造には影響しない。影響を受けるのは `lm show` の単体表示のみである。

## `LM_NOW`（時刻の固定）

- `LM_NOW`（RFC 3339）は**受け入れ試験と診断のための時刻注入**であり、利用者向けの手段ではない。設定すると、`lm` が内部で「現在時刻」を必要とする唯一の箇所（`lm show` の Compaction 判定、「Compaction（記憶の減衰）の読み方」）でその値を使う。未設定なら実時刻を使う。Compaction を回避して全文を見たいだけなら、`LM_NOW` ではなく `lm show <id> --full`（「Compaction（記憶の減衰）の読み方」）または `lm export` を使う。
- 不正な値を与えると `lm show` は Exit Code 1 の固定行エラーで止まる。
- `created_at` 等のレコードに実際に書き込まれる時刻には影響しない。
- `--updated-before` / `--updated-after`（docs/claim-recovery.md）は絶対時刻のみを受け取り相対指定を持たない。これらの値自体は `lm` が `LM_NOW` から自動導出するものではないため、`LM_NOW` を使う場合は呼び出し側（エージェント・シェル）が同じ基準時刻から両方の値を作って渡す。そうすることで `lm show`の Compaction 判定と `lm list --updated-before` 等の絞り込みが同じ「現在」を基準にでき、食い違わない。
