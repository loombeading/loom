# エージェントが頼ってよい lm の動作

以下は `lm` の動作の事実である。各項目は `lm` のテストが確かめている。

## 出力

- すべてのコマンドは Markdown だけを出力する。出力形式を切り替えるフラグは無い。
- 標準出力が端末でないとき、出力に装飾（ANSI エスケープ）を含まない。
- コマンドごとの見出しと行の項目構成（例: `lm list` の `- <ID> [<status>/P<n>] <title>`、`lm blocked` の `← blocked by: <ID>`、`lm show` の `## Description`・`## Dependencies`・`## History`）はリリース間で変えない。変えるときは互換性の無い変更としてリリースする。見出しと行の構成に頼ってよく、説明文・理由の文面には頼らない。

## JSONL の export と import

- `lm export` が出す JSONL を空のデータベースへ `lm import` し、もう一度 `lm export` すると、バイト単位で同じ JSONL になる。Gate も `type` と `blocks` のリンクを保って往復する。

## Ready の判定

- `lm ready` が返すのは、`open` で、未完了（`closed`・`cancelled` 以外）の `blocks` 依存先を持たず、未完了の `parent-child` の子を持たない Bead である。
- `closed`・`cancelled` の依存先と、取り除いた（`removed`）リンクは Ready を妨げない。子がすべて終端になった親は Ready に出る。
- Gate は `lm ready` に出ない。`lm ready` と `lm blocked` は互いに重ならず、両者を合わせると Gate を除く `open` の Bead の全体になる。

## claim と解放

- claim 済み（`in_progress`）の Bead は `lm ready` に出ない。claim した本人の `lm update <id> --release`、または他の主体の `lm update <id> --release --force` で `open` に戻り、再び `lm ready` に出る。
- 他の主体が claim した Bead は `--force` 無しでは解放できない。`--force` による解放は、通常の解放と区別して履歴に残る。
- 未完了の `blocks` 依存先を持つ Bead の `lm update <id> --claim` は Exit Code 1 で拒否され、状態と担当者は変わらない。
- 複数のプロセスが同時に `lm ready --claim` しても、同じ Bead を claim できるのはちょうど 1 つである。

## Gate の終端化

- Gate を終端にできるのは `lm gate resolve` と `lm gate reject` だけである。`lm close` と `lm update --status` は Gate に対して拒否される。Gate は claim できない。
- `lm gate resolve` と `lm gate reject` は `--reason` が無いと拒否される。`lm gate reject` は Gate 以外の Bead に対して拒否される。
- `lm gate reject` は、その Gate が塞いでいる `open`・`in_progress` の Bead をすべて `cancelled` にし、Gate 自体も `cancelled` にする。

## 非対話

- どのコマンドもエディタ・ページャ・確認プロンプトを起動しない。標準入力が閉じていても止まらずに終わる。
- 本文・理由などの長い入力は、フラグの値に `-` を渡すと標準入力から読む（例: `--reason -`）。

## 終了コード

- 成功は 0、拒否とエラーは 1 で終わり、エラーの内容は標準エラーに出る。
- `lm help <コマンド>` は 0 で終わる。引数なしの `lm` は使い方を標準エラーに出して 1 で終わる。
- `lm check gate-cycles` は、開いている Gate の待ち条件が解けなくなっている箇所を読み取り専用で列挙するコマンドである。違反とは次の 2 つを指す。1 つは、Gate の待ち対象を作る Bead がその Gate 自身に `blocks` や待ちで辿り着き、Gate が開くのを待ち合う閉路になっていること。もう 1 つは、Gate の待ち対象を外部参照に持つ未完了の Bead が 1 件も無く、待ち対象を作る者がいないこと。Gate が開かないまま残っている原因を調べるときや、定期的な点検に使う。違反が無ければ 0、違反があれば 1、データベースを読めなければ 2 で終わる。

## 読み取り専用モード

- 環境変数 `LM_READONLY` に空・`0`・`false`・`no` 以外の値を設定すると、書き込み（Bead・リンク・監査ログ・設定）をするコマンドは Exit Code 1 で拒否され、拒否したことが標準エラーに出る。`lm ready --claim` と `lm import` も拒否される。
- 読み取り専用モードでも、参照系のコマンド（`lm list`・`lm show`・`lm ready`・`lm export` など）は通常どおり動く。
