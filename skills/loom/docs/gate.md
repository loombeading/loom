## Gate（`lm gate`）の運用

- Gate は「待ち」だけを表す特別な Bead（`type = gate`）である。**claim も close もできない**（両方 Rejected で拒否される）。作業対象ではなく、他の Bead を塞ぐ待ち状態そのものを表現するための primitive であることを忘れない。
- **Gate は判断や完了の待ちがある Bead にだけ置き、待ちの無い Bead は Ready のまま残す。** Ready を減らすために**判断待ちの無い Bead に Gate を置くのは誤用**である。Gate が表せるのは「人・エージェント・外部の誰かの判断や完了を待っている」ことだけで、待ちが無いのに置くと、次にそれを見た者が「人が決めないと進めない」と誤読して止まる。止める理由が無いのに止まる。**利用者がセッションを閉じるなら Ready は Ready のまま残してよい**——仕様が固まって誰でも着手できる Bead が Ready に並んでいることは、欠陥ではなく正しい終了状態である。同じ理由で、着手しない Bead を claim して隠すのも誤り（次のセッションから作業中に見える）。
- **異常を見つけたり処理を終えたりしたときは、自分で回復して先へ進める。人向け Gate（`--kind human`・`confirm`）は、エスカレーションしても解けないときにだけ作る。**「何かしたら Gate で知らせる」「失敗が続いたら Gate で報告する」という通知目的の Gate は作らない。代わりに次の順で処理する。
  1. 検知した側（実行者・セッション）が原因を分類し、既知の原因にはその場で回復処理を当てて再開する（claim・lock の解放、worktree の掃除、backoff 付きの再試行など）。回復の分岐はコードに落とす。
  2. 分類できない原因や、直すのに実装が要る原因は、根本原因を調べる Bead を起票して実行者に渡し、自分は backoff を付けて続ける。
  3. 選択や可否の判断が要るときは、判定者（`--kind adjudicate`・adjudicate エージェント）に渡す。
  4. 判定者の結論を得ても、利用者にしかできない操作（課金・外部 repo の merge 権限・秘密情報の入力など）が残るときに限って人向け Gate を作る。`--current` には 1〜3 で試したことを書く。経過と結果は Bead の履歴（`--reason`）や実行者自身のログに残す。人に知らせる手段として Gate を使わない。
- **人向け Gate が待つ操作が PR の上で済むときは、手順を PR 本文の「## 利用者の操作」節に書き、Gate の説明（`--current`・`--check`）は「PR #N の「利用者の操作」節の手順を確認して実行する」の 1 文にする。** 節には、機械が確かめた事実（チェックボックスにしない）、判断が要る項目だけの `- [ ]`、すべてチェックしたら付ける承認の印（ラベル）とその後に起きることを書く。承認の印に使うラベルと、印を見て Gate を resolve する担い手は実行者の文書が定める。基準: 手順が Gate と PR に分かれていると、CLI を打てない利用者が何をすれば閉じるか分からず承認が止まる。
- `lm gate create --blocks <id>[,--blocks <id>...] --subject "<件名>" [--current "<現状>"]
  [--proposal "<提案>"] [--check "<チェックすると>"] [--resolver "<誰が resolve するか>"]
  [--reason "..."] --kind human|external|adjudicate|confirm`で単独の Gate を作れる（1 つ以上の `--blocks` が必須。`--subject` も必須。`--kind` は必須で既定は無い。付けなければ exit 1。`--kind external` は `--resolver` も必須で、無ければ exit 1。`--reason` は任意）。`--current`/`--proposal`/`--check`/`--resolver`/`--reason` は省略でき、Loom は中身を解釈しない自由記述である。 Gate の型は `--kind` で選ぶ: `human` はラベル `kind:human`、`external` はラベル `kind:external`、`adjudicate` はラベル `kind:adjudicate`、`confirm` はラベル`kind:confirm` が付く（`human`・`confirm` は `lm gate` の「Needs you」節に、`adjudicate` は判定者（選択や可否を決めるエージェント）の結論を待つ「Waiting on judge」節に入る。`adjudicate` は判定者の結論で開き、人の手を要しない）。型の判定はラベルのみで決まる（`--subject` 等の文面は見ない）。ラベルの無い Gate（インポート等で持ち込まれたもの）は「Unknown kind (fix labels)」節に入る。
- **Gate の Title は `--subject` から自動生成される**: `gate: <件名の先頭 60 rune>`（60 rune を超える場合は末尾を `…` に置き換える）。`--current`/`--proposal`/`--check`/`--resolver` を含む全文は切り詰められず `description` に残るため、`lm gate` や`lm show <id>` で確認できる——Title は `lm list` の列挙表示のためだけ短くしている。
- **Gate を解除する方法は 1 つだけ**: `lm gate resolve <id> --reason "..."`（`--reason` は必須）。`lm close` や `lm update --status` では解除できない。例外: 待ち条件がもう満たされた Gate は `lm close` と `lm gate sweep` が自動で resolve する（下の節）。
- **誰でも解除できる**（claim/release のような担当者制限は無い）。Gate には「復旧経路の無いまま誰の `lm ready` にも二度と現れない」という事故を防ぐ意図があるため、担当者を絞らない設計になっている。
- **時間経過では開かない**。`LM_NOW` を進めても Gate は自動で解除されない（docs/memory.md の Compaction とは無関係の別機構）。誰かが明示的に`lm gate resolve` を呼ばない限り、待ち続ける。例外: 待ち条件がもう満たされた Gate は、時間経過ではなく `lm close`・`lm gate sweep` の実行が引き金になって resolve される（下の節）。
- **external 型の Gate は watcher が resolve を呼ぶ**ことを想定する。watcher は Loom の外で条件を監視し、満たされたら `lm gate resolve` を呼ぶプロセスで、実行者が用意する（Loom 本体は提供しない）。CI の完了・外部システムのコールバックなど、Loom の外で条件が満たされたことを検知した側（人間ではなく監視プロセス）が`lm gate resolve --reason "<検知した条件>"` を実行する運用にする。
- `lm gate` は**未解除（open）の Gate のみ**を、種別ごとの節へ分けて一覧する: `## Needs you`（human・confirm）→ `## Blocked by other work`（human・confirm のうち、Gate 自身に未完了のブロッカーがあるもの、または終端でない塞ぎ先のどれにも他の未完了のブロッカーが残るもの。終端の塞ぎ先は判定に使わない）→ `## Waiting on judge`（adjudicate。実行者が判定者に回す）→ `## Opens automatically`（external）→ `## Unknown kind (fix labels)`（ラベルの無い Gate）。空の節は出さない。`lm ahead` の `## Gate waiting` も同じ分類を使い、Gate ごとの行は `### Needs you` にだけ出し、他の分類は分類ごとに件数の 1 行に畳む。Blocked by other work の Gate は ahead の `### Needs you` にも出ない。各行の直後に2空白インデントの補助行が付く: human・confirm・adjudicate は「When resolved: ...」と「Run: lm gate resolve ...」、 external は塞いでいる各 Bead の「Waiting for: <id> <status>」（子 Bead がいれば終端数の内訳つき）、Unknown kind (fix labels) は「Fix: lm update <id> --add-label kind:adjudicate|kind:external」。
  ```
  ## Needs you
  - id-2f5 [gate/open/P2] production へのデプロイ承認を待つ  → blocks: id-ffy
    - When resolved: (--check に記載なし)
    - Run: lm gate resolve id-2f5 --reason "go"
  ```
  解除済みの Gate は `lm show <id>` の履歴でのみ追える。`--all` は互換のため受け付けるが効果は無い（全種別は常に出る）。`lm ahead` の `--all` はこれと違い、`## Milestones` 行の `gate=` を human 型だけでなく全種別の Gate に塞がれた件数に広げる（[pr.md](pr.md)「PR 運用」）。利用者が任意の拡張として「人が操作すべき Gate の件数」をどこかに表示するときは、「Needs you」節の行だけを数える（Waiting on judge・Opens automatically・Unknown kind (fix labels) は数えない）。Loom が提供するのは件数の元になる `lm gate` の出力だけである。
- 無人の実行者は、実行ごとに `lm gate` を読んで open な Gate を自分のログや通知へ出す。対話セッションを開かなくても待ちに気付ける。出し方（ログの置き場・通知の手段）は利用者が自分で定める。
- 進捗見通しで利用者にしかできないことを答えるときは、`lm gate` の「Needs you」節の行だけを引き写す。Gate の一覧を読み直して操作を推測で並べると、前提の残る Gate を「いま操作できる」と伝えてしまう。
- 人の操作を待つ Gate を作るときは、その操作に要る外部の前提を、`wait:` ラベルを持つ Gate か `lm dep add --type blocks` で表す。前提を本文にだけ書くと、Gate は「Blocked by other work」に入らず「Needs you」に出る。

### external 型 Gate の resolve 担当

external 型（`--kind external`）は、watcher（上で定義した外部監視プロセス）が resolve を呼ぶ場合と、待ち条件を満たしたセッションが、そのセッションの中で `lm gate resolve` まで行う場合の両方がある。後者では、作成者と満たした側が別セッションであっても、resolve は満たした側が打つ——満たした後に別セッションへ引き渡して resolve を頼まない。

作成者は `--resolver` に「満たしたら誰が resolve するか」を書く（例: `受入を終えたセッション`）。`--kind external` で `--resolver` を付けずに作成しようとすると `lm gate create` が exit 1 で拒否する。

Gate の持ち主は、その Gate を作った actor（Gate の履歴の created に記録された actor）である（[用語集](https://github.com/loombeading/loom/blob/main/docs/glossary.md)「Gate の持ち主」）。lm が監査ログに記録する actor（`--actor`・`LM_ACTOR`。どちらも無いときは実行環境が決める既定値）がそのまま持ち主の印になるので、対話セッションが持ち主のまま待つ Gate を作るときも、別の印は要らない。

持ち主のセッションが終わったまま open に残った external Gate を孤児 Gate と呼ぶ（[用語集](https://github.com/loombeading/loom/blob/main/docs/glossary.md)「孤児 Gate」）。実行者は、持ち主の actor が今も動いているかを測り、止まっていると確かめられたときだけ孤児 Gate を resolve する。確かめられないときは resolve せず、根本原因の Bead を起票する。

「動いている」の判定基準は lm が定めず、利用者が実行者ごとに決める項目である。たとえば次のどれか、または組み合わせを基準に選べる。

- 実行者が起動したプロセスがまだ生きているか
- その actor が監査ログに最後に書き込んだ時刻が、利用者の決めた時間より新しいか（`lm show <id> --full` の `## History` で読める）
- その actor が claim している Bead の `claim_expires_at` が切れていないか

基準: 持ち主のセッションが終了・中断などで失われると、誰も resolve しないまま下流の Bead を塞ぎ続ける。

### 別の Bead の完了を待つときは依存で表す

待ち対象が別の Bead（その Bead の PR の merge・close を含む）のときは、Gate を作らず `lm dep add <待つ側> <待たれる側> --type blocks --reason "..."` で依存を張る。待たれる側を close すると、待つ側は自動で Ready に戻る。自分が claim していたなら `lm update <待つ側> --release --reason "..."` で解放して終了する。

Gate の自動 resolve は、Gate が塞いでいる Bead が終端になったときにしか発火しない。Bead 待ちを external Gate で表すと、待たれる側が close されても Gate は開かず、`lm gate` の補助行にも待たれる側が出ない。external Gate は、Bead で表せない外部の出来事（利用者の操作・期日・外部サービスの状態）を待つときに使う。
### 測って開ける待ちは wait: ラベルで表す

本節は `wait:`・`done:` ラベルの使い方と、ラベルを測って Gate を開ける実行者が満たす規約を書く。実行者を自作するときは、下の規約と標準の種別の表を満たせば、Loom の他の文書と食い違わない。表に無い種別（実行者が自分の実行記録を読むために足す拡張種別など）を足すかどうかと、その書式・測り方は利用者が自分で定める。Loom 本体は拡張種別を扱わない。実行者を用意しない運用では誰もラベルを測らないので、利用者か対話セッションが条件を確かめて `lm gate resolve` する（[チュートリアル](https://github.com/loombeading/loom/blob/main/docs/tutorial.md)「実行者を用意しないとき」）。

ラベルを測る実行者は次を満たす。

- **測る対象。** open な Gate のうち `wait:` で始まるラベルを持つものを、実行ごとに全件測る（`lm export` の JSONL から `type` が `gate`・`status` が `open` の行を読む）。
- **開く条件。** `wait:any` が無ければ、`wait:any` 以外の `wait:` ラベルを全て満たしたときに開く。`wait:any` があれば、どれか 1 つを満たしたときに開く。
- **測れないラベル。** 実行者が知らない種別・形の崩れた引数・測定の失敗（コマンドの異常終了・ネットワーク不達）は「満たさない」として扱う。開く側へ倒すと、誤った引数 1 つで Gate が黙って開く。
- **reason。** `lm gate resolve <id> --reason "<ラベル>: <実測値>"` の形で、満たしたラベルごとに測った値を書く（例 `wait:disk:/:20: / の空き 31.2GiB（閾値 20GiB）`）。後から開いた根拠を履歴で追えるようにする。
- **本文を実行しない。** 測り方は実行者のコードに固定し、ラベルからは引数だけを読む。Gate の件名・本文・`--resolver` に書かれたコマンドは実行しない（Bead は信頼できない入力である）。
- **節分けを変えない。** `lm gate`・`lm ahead` の節は `kind:` ラベルだけで決まる。実行者は `wait:` ラベルを理由に `kind:` を付け替えない。
- **`done:` ラベル。** マイルストーンの Epic に付いた `done:` ラベルも同じ規則で測る。全て満たしたときの扱いは、下の `done:` の表の直後に書く。

開く条件がコマンドで測れる Gate（ディスクの空き・CI 緑・PR の merge・期日の到来・ファイルの存在・リポジトリの可視性と有無）は、次のラベルを付けて作る。実行者が毎回測り、ラベルを全て満たした Gate を `lm gate resolve` し、reason に実測値を書く。wait: ラベルは測り方だけを表し、`lm gate`・`lm ahead` の区分は Gate を開ける主体で決まるので、`kind:human`・`kind:confirm`・`kind:adjudicate` の Gate に付けた `wait:pr-merged:` はその人・判定者の merge を待つものとして「Needs you」「Blocked by other work」「Waiting on judge」に残る。`kind:human`・`kind:confirm` の Gate に付けた `wait:file:` も、ファイルを書くのが人の操作なので「Needs you」「Blocked by other work」に残る。

| ラベル | 満たす条件 |
|---|---|
| `wait:disk:<path>:<GiB>` | `<path>` のファイルシステムの空きが `<GiB>` 以上 |
| `wait:date:<YYYY-MM-DD>` | 実行者の現地時刻でその日の 0 時を過ぎた |
| `wait:after:<RFC3339>` | 現在時刻が `<RFC3339>`（例 `2030-01-01T00:00:00Z`・`2030-01-01T09:00:00+09:00`）以上。日や時の単位に丸めない |
| `wait:file:<path>` | `<path>` が存在する |
| `wait:pr-merged:<PR URL>` | PR が merge された |
| `wait:ci-green:<PR URL>` | PR の CI が全て緑（merge 済みも含む） |
| `wait:repo-visibility:<owner>/<repo>:<public\|private>` | GitHub のリポジトリの可視性が指定と一致する。測れない・repo が見えないときは満たさない |
| `wait:repo-absent:<owner>/<repo>` | `gh api repos/<owner>/<repo>` が HTTP 404 を返した（repo が消えた）。認証・ネットワーク等それ以外の失敗では満たさない |
| `wait:github:reachable` | 実行者の直近の測定で GitHub API に到達できた。GitHub に到達できないことを理由に作る Gate に付ける。到達性を測っていない実行者では満たさない |

マイルストーンの Epic の完了条件 `done:<種別>:<引数>`（[milestone.md](milestone.md)）も実行者が上の表と同じ測り方で毎回測る。Epic の完了条件にだけ使える標準の種別が 1 つある。

| ラベル | 満たす条件 |
|---|---|
| `done:tag:<版>` | タグ `<版>`（`v` で始まる）の GitHub Release が draft でなく公開されている |

open のまま全て満たしたら、実行者は `kind:adjudicate` の Gate でその Epic を塞ぎ、close と完了条件に要らない子の移し先を判定者に決めさせる。

Gate はラベルを全て満たして開く。「期日かファイルの先に来た方」のようにどれか 1 つで開きたいときは、同じ Gate に `wait:any` も付ける（例: `wait:any`・`wait:date:2030-01-01`・`wait:file:<path>`）。Gate を条件ごとに分けると、塞がれた Bead は両方の resolve を待つので、どれか 1 つでは開かない。

計測の窓（起点の時刻＋期間。例「merge（2030-01-01T00:00Z）後 1 週間」）が揃うのを待つ Gate は、窓の終わりの時刻を `wait:after:<RFC3339>` で付け、件数が要るなら実行者の拡張種別を並べる。`wait:date:` は日単位に丸めるので、窓の終わりより早く開くか、翌日まで遅れて開く。`lm update <Gate> --add-label wait:date:<日付>` を付けるとき、その Gate の件名・本文に時刻（`HH:MM`）があれば、lm は stderr に `wait:after:` を勧める警告を出す（ラベルは付き、exit 0）。

- 人の操作が要らずに満たされる待ちは `--kind external --resolver <測る実行者>` で作る。人の操作（PR の merge 等）で満たされる待ちは `--kind human` のまま、完了を測れるラベルを付ける。どちらも実行者が測って開く。
- 全ラベルが原因を共有する型（`wait:disk`・`wait:file`・`wait:pr-merged`・`wait:ci-green`・`wait:repo-absent`・`wait:github`）だけの Gate は、作る前に `lm list --type gate --label <ラベル>` で同じラベルの open な Gate を探す。あれば新しく作らず、`lm dep add <塞ぐ Bead> <既存の Gate> --type blocks` で既存の Gate に塞がせる。
- それでも同じラベル集合の open な Gate が 2 つ以上できたら、全ラベルが上の原因共有型であるときに限り、実行者が最も古い Gate を残し、新しい方が塞いでいた Bead をそちらへ `lm dep add` で付け替えてから新しい方を resolve する。
- 目的ごとに値を直しうる型（原因共有型以外の全て。標準では `wait:date`・`wait:after`、実行者の拡張種別も含む）を 1 つでも持つ Gate は Bead ごとに作り、既存の Gate への流用も実行者の統合もしない。期日や件数を共有すると、片方の目的で値を直したときにもう片方の待ちも黙って動く。
### 自動 resolve（lm close・lm gate sweep）

open な Gate が次を満たすと、`lm close` と `lm gate sweep` が自動で `lm gate resolve` を呼ぶ。満たさなければ何もしない。

Gate が塞いでいる Bead（`--blocks`）が 1 件以上あり、全て終端（closed・cancelled）になった。reason は `自動: 塞いだ Bead が全て終端`。

`lm close` は、閉じた Bead またはその親を塞いでいる open Gate をその場で判定する。`lm gate sweep` は開いたままの Gate 全件を判定する。無人の実行者は実行ごとに `lm gate sweep` を呼び、close の経路を通らない Gate（後から `--blocks` を足した等）も次の実行で拾う。
### 子待ち Gate は作らない

親 Bead に終端でない子（`parent-child`）があるかぎり、親は `lm ready` に出ない（[guarantees.md](guarantees.md)「Ready の判定」）。`lm blocked` には子が `← blocked by:` として並ぶ。順序も可視化もこれで足りるので、**『子 Bead の完了待ち』Gate は作らない**。子が全て終端になれば親が Ready に出るので、そこで claim し、親に固有の作業が残っていないことを確かめてから `lm close --summary` で閉じる。

### Gate 同士の順序は依存で表す

Gate は「開く／開かない」しか表さず、Gate 同士の順序は Gate からは読めない。どちらを先に開くかが決まっているなら、エージェントは Bead 間の依存で表す義務を負う。後に着手するものを第 1 引数、先に終わるものを第 2 引数にして `lm dep add <後の id> <先の id> --type blocks` を張る。張ってあれば `lm gate` の一覧からどの Gate が何を塞いでいるかが読め、エージェントが自分で順序を決められる。

### 開かない Gate を点検する（`lm check gate-cycles`）

Gate が開かないまま残っていて、待ち対象を作る Bead が見当たらないときや、待ちが閉路になっていそうなときは、エージェントが `lm check gate-cycles` を打つ。このコマンドは `lm help` の一覧に `check gate-cycles` として載っており、データベースを書き換えずに、開いている Gate の待ちが自分自身へ戻る閉路と、作る者のいない待ち対象を列挙する。違反の定義と終了コードは [guarantees.md](guarantees.md) にある。違反が出たら、閉路なら `lm dep remove` で輪を切るか待ち対象を作る Bead を別に起票し、作る者がいない待ち対象なら、それを外部参照に持つ Bead を起票するか Gate の待ち条件を直す。無人の実行者は定期的な点検としてこれを呼んでよい。

### Gate の整理（`lm gate` を見たとき）

`lm gate` の一覧を見た対話セッションは、**推奨を並べて利用者の指示を待たない**。人向け Gate を一覧して「resolve してよいか」と聞き返すと、Gate を作った側が次の誰かへ渡したのと同じ構造になり、待ちが一段増えるだけで誰も解除しない。Gate を次の 3 つに分け、分類ごとに対話セッションが自分で処理する。

1. **待ち条件がもう満たされている**（PR が受入済み・agent の作業が終わっている）。実物（PR・worktree・引き継ぎ簿・`lm show` の履歴）で確かめ、確かめた事実を `--reason` に書いて `lm gate resolve` する。
2. **判断材料が Bead に揃っている**（`kind:human`・`kind:confirm` で、`--current`/`--proposal`/`--check` に現状・提案・チェックすると何が始まるかがあり、推奨案があり、分類 3 に当たらない）。対話セッションが判定して `lm gate resolve` する。採らないなら判定者（`adjudicate`）に振る。判定者は結論と理由を `lm update --reason` で記録するだけで、`lm gate reject` はその結論に沿って判定者を起動した側（対話セッション・実行者の判定起動）が実行する。実装を受け持つ実行者の起動（設計・再開・受入）は reject せず、`kind:adjudicate` の Gate で塞いで終わる。
3. **利用者にしか出来ない操作を待っている**（公開・外部への送信・force push・main への直接 push・ブランチ削除など、取り消せない操作や利用者の権限が要る操作）。`accepted` の付与は実行者の受入が行うので、ここに入らない。1 と 2 を全て片付けてから、利用者へは「何をすれば次に何が始まるか」を操作ごとに 1 行で列挙する。それ以外を混ぜない。

処理のたびに、放置の原因（作成側と条件を満たした側が別セッションだった・判定が誤っていた・通知経路が無かった）を Bead に起票する。解除だけして原因を残さないと同じ Gate が再び溜まる。Gate の本文が現状と食い違っていれば `lm update <gate-id> --reason` で経過を記録する。

実行者は、分類 2 の条件（`kind:human`・`kind:confirm` で、`--current`・`--proposal`・`--check` がすべてある）を満たす Gate を対話セッションへ示し、本節の処理を促してよい。これは任意の拡張で、示すかどうかと示す手段は利用者が自分で定める。

### Waiting on judge の処理

`kind:adjudicate` の Gate は、人ではなく判定者を起動した側が開く。起動した側は Gate の件名・現状・提案と塞いでいる Bead を判定者に渡し、次のいずれかまで進める。判定者は結論と理由を `lm update --reason` で記録するだけで、resolve・ラベルの付け替え・起票は判定者を起動した側が結論に沿って行う。

1. **resolve** — 判定が go で、開けば塞がれた Bead がそのまま進められる。判定の結論を `--reason` に書いて `lm gate resolve` する。
2. **直し Bead の起票・観測待ちへの付け替え** — 判定が「このままでは開けない」で、エージェントが直せる。直す Bead を起票して Gate の前提に `lm dep add` で張り、Gate は open のまま残す。測れば開ける待ちなら、`wait:` ラベル（「測って開ける待ちは wait: ラベルで表す」）を付けて実行者に測らせる。
3. **人への格上げ** — 開くのに人にしか出来ない取り消せない操作（公開・外部への送信・force push・main への直接 push）が要る。それ以外の判断（変更を受け入れるか・出荷してよいかを含む）は判定者の結論で 1 か 2 に進め、人へ回さない。判定の結論と人に求める操作を `--reason` に書いて判定 Gate を `lm gate resolve` し、`lm gate create --kind human --adjudicated-by <判定 Gate> --blocks <塞いでいた Bead>` で人の Gate を新しく作る（判定 Gate のラベルを `kind:human` へ付け替えると lm が拒否する）。`wait:` ラベルは新しい Gate へ引き継ぐ。以後は「Needs you」に出る。

無人の実行者が Waiting on judge を自分で回すときも、判定者に渡すもの（件名・現状・提案・塞いでいる Bead）と、結論に沿って行う 1〜3 は上と同じである。判定者をいつ起動するか（実行ごと・件数が溜まったとき等）と、判定者に使うエージェントは利用者が自分で定める。

### Gate の通知（任意の拡張）

`lm` 本体は open な Gate を通知しない。通知が欲しい利用者は、`lm gate` の出力を読んで知らせる仕組みを自分で用意する。その仕組みは Gate の状態を変えない（resolve・ラベルの付け替えは上の各節の手順で行う）。

### Gate の型

- `lm gate create --kind human|external|adjudicate|confirm`（必須。既定は無い）で Gate の型を選べる。`confirm` は `human` の下位区分（`lm gate` では同じ「Needs you」節に入る）、`adjudicate` は判定者の結論に沿って判定者を起動した側が開く型（「Waiting on judge」節に入り、実行者が判定者に回す。上の「Waiting on judge の処理」）で、4種すべてに対応する `kind:` ラベル（`kind:human`・`kind:external`・`kind:adjudicate`・`kind:confirm`）が付く（スキーマ変更なし）。`human`・`confirm` は `--adjudicated-by <closed の kind:adjudicate Gate>` が必須で、`kind:human`・`kind:confirm` を `lm create --type gate --label`・`lm update --add-label` で付けるときも `adjudicated-by:` ラベルが同じ条件を満たさなければ拒否される。型の判定はラベルのみで決まり、`--subject` 等の文面は見ない。ラベルの無い Gate は `lm gate` の「Unknown kind (fix labels)」節に入る。`adjudicate` は `lm gate resolve` のみで開く（誰が resolve するかは「Waiting on judge の処理」）。
- 確認型（`kind:confirm`）の Gate は、実行者の機械判定が全て満たされたときに resolve される。塞がれた Bead は無人の実行者が拾わないので、対話セッションが claim せずに着手し、PR 作成・判定・resolve 後の merge 確認・close まで進める。resolve されなければ PR は draft に戻して人の resolve を待つ。機械判定に使うコマンド（テスト・lint 等）は利用者が自分で定め、全て exit 0 のときだけ resolve し、reason に走らせたコマンドと結果を書く。
