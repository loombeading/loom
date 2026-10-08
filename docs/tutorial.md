# Loom チュートリアル — エージェントに任せ、あなたは [Gate](glossary.md#gate) だけ見る

Loom（コマンドは `lm`）は、計画（[Bead](glossary.md#bead)・[依存](glossary.md#依存)・Gate）を保存し、人とエージェントが読み書きするタスク管理ツールです。Loom 自身は判断も実行もせず、計画に沿って作業を進めるのはエージェントです。人間が日常的に見る画面は、`lm gate` と `lm ahead` の 2 つで足ります。

ここに載せたコマンドと出力は、実際に一時ディレクトリで `lm` を実行して得たものをそのまま貼っています。`...` の行は、出力の一部を省いたところです。手元で試すときは、普段使っているデータベース（`LM_DIR`、未設定なら `~/.local/share/loom`。README.md の「5 分で使う」を参照）を汚さないように、`LM_DIR` を作業用の一時ディレクトリに絶対パスで向けてください（例: `LM_DIR=/tmp/tut/.loom`）。

## あなたがやること

人が触るのは次の 3 つだけです。

- `lm gate` を見て、resolve（やる）か reject（やらない）で答えます。条件を添えてもかまいません（`--reason`、または対話での一言）。
- 眠る前に `lm ahead` を見ます。
- 次にやりたいことや[マイルストーン](glossary.md#マイルストーン)を、`lm create` で積みます。

これ以外の作業（Bead の分割・依存の設定・実装・PR 作成・merge の確認・クローズ）は、エージェントへ委譲できます。

## 始める

`lm init` でデータベースを作る手順は、[README.md](../README.md) の「5 分で使う」を参照してください。

続けて README.md の「エージェントで使う」の手順で推奨ルール（loom skill）をエージェントに入れると、以降はエージェントがそれに従って `lm` を読み書きします。

## 判断待ちに答える

エージェントが人間の判断を待っているときは、`lm gate` に次のように出ます。

この例の `id-k4m` は、エージェントが loom skill の手順に沿って `lm gate create` で作った Gate です。Gate を作るのはふだんはエージェントの仕事です（Gate の種類や作成手順は [skills/loom/docs/gate.md](../skills/loom/docs/gate.md) を参照してください）。`lm init` した直後の DB には Gate が無いので、手元で同じ出力を見たい場合は、後述の「眠る前に」にあるコマンド列を実行してください。その中の `lm gate` の行がこの出力を出します。Bead の ID は読者の環境では別の値になります。

```
$ lm gate
## Needs you
- id-k4m [gate/open/P2] 利用規約の改訂を公開してよいか  → blocks: id-z09.2  via id-z09.2: id-z09  eff=P1(id-z09)
  - When resolved: 利用規約の改訂を公開する作業を始める
  - Run: lm gate resolve id-k4m --reason "go"

## Opens automatically
- id-w7r [gate/open/P2] 価格の改定日を待つ  → blocks: id-z09.3  via id-z09.3: id-z09  eff=P1(id-z09)
  - Waiting for: id-z09.3 open
  - Waiting for: wait:date:2026-09-25
```

`## Opens automatically` の Gate は、条件がそろえば開くのであなたの操作を待っていません。

1 行目には件名と、この Gate が塞いでいる作業が出ており、その下に「チェックすると何が始まるか」と答え方が出ています。現状と提案は `lm show <id>` の本文で読めます。返事は次の二択のどちらかです。

- やる場合は `lm gate resolve <id> --reason "go"` を実行します。
- やらない場合は `lm gate reject <id> --reason "..."` を実行し、Gate が塞いでいた作業ごと取り下げます。

二択には条件や方針を添えてかまいません（例:「go。ただし公開後を前提に書く」）。返事はコマンドに限らず、エージェントとの対話で「go」「やらない」と一言返すだけでも伝わります。

```
$ lm gate resolve id-k4m --reason "go"
- Resolved: id-k4m
## Newly ready
- id-z09.2 [open/P2] 利用規約の改訂を公開する  eff=P1(id-z09)
```

「眠る前に」の `lm ahead` の出力例は、この resolve を打つ前の状態です。手元で resolve を試すのは、その出力例と見比べたあとにしてください。

`lm gate` に出ていない待ち（`lm blocked` など）は、エージェントが自分で判断して進めるためのものなので、人間が追う必要はありません。

「〜になったら人間待ちにして」と条件を添えて頼んでおけば、条件がそろうまでその Gate は「Needs you」（あなたの操作待ち）に出ません。

## 眠る前に

今夜エージェントに任せて寝る前には、解消すべきブロッカーが残っていないかを確かめます。そのためには `lm ahead` を引数なしで実行するだけで足ります。

次の出力例は、リリース `id-z09` の下に作業が 3 件あり、そのうち 1 件があなたの判断を、もう 1 件が期日（9 月 25 日）の到来を待っている状態です。

同じ形は、まだ何も記録していない `LM_DIR` で次のコマンドを実行すると作れます。`<…の ID>` には、それより前のコマンドが `Created:` の行に出した ID を入れます。期日を過去の日付に固定しているので、いつ実行しても出力例と同じ行が並びます。読者の環境で値が変わるのは、Bead の ID、`Now:` の時刻、`## Outlook` の `Window:` 行の 3 つです。出力例は `Now:` を 2026-09-26 に固定して実行したものなので、`Window:` 行は作ったばかりの Bead を数えていません。今日実行すると、直前に作った 4 件が数えられ、`created=4 → 0.00/day in 1.33/day` になります。

```
lm create --title "2026-Q4 リリース" --priority 1 --label milestone:2026-Q4
lm create --title "検索結果のハイライト表示を追加する" --priority 1 --parent <リリースの ID>
lm create --title "利用規約の改訂を公開する" --priority 2 --parent <リリースの ID>
lm create --title "価格表を更新する" --priority 2 --parent <リリースの ID>
lm gate create --kind adjudicate --subject "利用規約の改訂は人の判断が要るか" --blocks <利用規約の Bead の ID>
lm gate resolve <判定の Gate の ID> --reason "人が判断する"
lm gate create --kind human --adjudicated-by <判定の Gate の ID> --subject "利用規約の改訂を公開してよいか" --check "利用規約の改訂を公開する作業を始める" --blocks <利用規約の Bead の ID>
lm gate create --kind external --resolver agent --subject "価格の改定日を待つ" --blocks <価格表の Bead の ID>
lm update <価格の改定日を待つ Gate の ID> --add-label wait:date:2026-09-25
lm gate
lm ahead
```

人の判断を待つ Gate（`--kind human`）は、判定役が「人の操作が要る」と結論して閉じた Gate を `--adjudicated-by` に渡して作ります。期日を待つ Gate は、`wait:date:<YYYY-MM-DD>` のラベルを付けて作ります。この Gate は `- Opens automatically:` に数えられ、`### Needs you` には出ません。`## Outlook` の `gates:` には `date=1(2026-09-25)` のように待っている日付が出ます。日付を過ぎても開いていなければ、`- Stalled:` の行にも出ます。上のコマンドは日付を過去の 2026-09-25 にしているので、今日実行しても `Stalled:` の行が出ます。期日がまだ来ていない Gate を見たいときは、`wait:date:` に明日以降の日付を渡します。そのときは `- Stalled:` の行が出ず、Gate は `- Opens automatically:` にだけ数えられます。

```
$ lm ahead
# Ahead
Now: 2026-09-26 09:00:00 JST

## Ready
- id-z09.1 [open/P1] 検索結果のハイライト表示を追加する  est=n/a(no-data)  milestone=2026-Q4

## In progress
(none)

## Summary
- In progress: 0 tokens (0 beads)
- Ready: 0 tokens (1 beads)
- Courses: 0 tokens (0 beads in 0 courses)
- Gate waiting: 0 tokens (3 beads)
- Milestones: 1
- Budget: n/a (fewer than 3 recorded days; pass --budget)

## Outlook
- Window: 3d closed=0 created=0 → 0.00/day in 0.00/day, n/a created per close
- P1: n=1 cum=1 stop=見込み無し cont=見込み無し  gates: date=1(2026-09-25) human=1

## Milestones
- Window: 72h closed=0(PR 0) → 0.00〜0.00/h
- id-z09 [open/P1] 2026-Q4 リリース  milestone=2026-Q4  ready=1 wip=0 course=0 gate=1 remaining=3  close/日=0.0〜0.0  残り日数=見込み無し  eta=発散  Gate 待ち

## Gate waiting
- Opens automatically: 1 gates, waiting=2 est=n/a(no-data)
- Stalled: id-w7r (wait:date passed but still open)

### Needs you
- id-k4m [gate/open/P2] 利用規約の改訂を公開してよいか  → blocks: id-z09.2  via id-z09.2: id-z09  eff=P1(id-z09)  waiting=2 est=n/a(no-data)
```

`Now:` は `lm ahead` を実行した時刻で、実行したマシンのタイムゾーンで表示されます（例は日本時間の環境で実行したものです）。`est=` は、その作業を片付けるのに要るトークン数の見積りです。見積りは完了した作業の実績（[token cost](glossary.md#token-cost)）から出すので、使い始めてまだ実績が無いうちは `est=n/a(no-data)`（見積りに使えるデータが無い）と表示され、`## Summary` の合計も 0 tokens になります。

節は下へ行くほど大事な順に並んでいるので、出力の末尾から上へ読んでください。見るところは次の 3 つに絞れます。

1. 末尾の `## Gate waiting` の `### Needs you` に Gate の行が残っていれば、寝る前に `lm gate resolve`（やる）か `lm gate reject`（やらない）で決めておきます。決めずに寝ると、その Gate が塞いでいる作業は朝まで止まったままになります。例では `id-k4m` が該当し、`waiting=2` はこの Gate が止めている作業の件数です。その上に並ぶ `- Opens automatically: 1 gates ...` のような 1 行は、あなたの操作を待たない Gate の件数なので読み飛ばしてかまいません。ただし `- Stalled:` の行は、期日を過ぎても開かない Gate があるという異常を示しているので、エージェントが止まっていないかを確かめてください。例では 9 月 25 日を待つ `id-w7r` が、翌日になっても開いていません。何も待っていなければ、`## Gate waiting` の下は `(none)` の 1 行だけになります。
2. `## Ready` が空であれば、次に片付けたいアイデアを `lm create` で積んでおきます。
3. `## Milestones` の先頭行（優先度が最も高く、同順なら残りが最も少ないもの）が、目の前のリリースです。`remaining=` の残数を見ながら、`## Ready` のうち `milestone=<名前>` の印が付いた行から着手すれば、そのリリースが進みます。[`ready`](glossary.md#ready) は着手可能な数、[`wip`](glossary.md#wip) は着手中の数、[`course`](glossary.md#course) は Ready の次以降の段（Course 1・Course 2 …）に含まれる件数、[`gate`](glossary.md#gate) は人の判断を待つ Gate（human 型）に塞がれている件数です。数えるのは Gate の数ではなく塞がれた Bead の数で、`lm ahead --all` を付けると全種別の Gate に塞がれた件数になります。`gate` が 0 でなければ、塞いでいる Gate が 1 の `lm gate` にも出ています。なお `lm gate` の `--all` は互換のためのフラグで、付けても出力は変わりません。

これ以外の行（上のほうに出る Course の番号や見積りの内訳など）はエージェントが自分の判断のために読むものなので、人間が読む必要はありません。画面の上に流れて見えなくなってもかまいません。

## 仕事を積む

次にやりたいことを思いついたら、次のように積んでおきます。

```
$ lm create --title "検索結果のハイライト表示を追加する" --priority 2
- Created: id-ag1 [open/P2] 検索結果のハイライト表示を追加する
```

`--priority`（`-p`）の既定は P2 なので、急ぐときは 1 を指定してください（0 は指定できません）。

複数の [Epic](glossary.md#epic) をまとめたリリース単位を作りたいときは、リリース用の Bead に `milestone:<名前>` ラベルを付けます。

```
$ lm create --title "2026-Q4 リリース" --priority 1 --label milestone:2026-Q4
- Created: id-z09 [open/P1] 2026-Q4 リリース
```

進み具合は、「眠る前に」で見た `lm ahead` の `## Milestones` で確かめられます。

## 運用が育ってきたら

複数のプロジェクトや複数の端末で使い始めたとき、[実行者](glossary.md#実行者)を用意せずに使うときは、次の節を読んでください。

### 複数プロジェクトを 1 つの DB で

1 つの Loom データベース（`$LM_DIR/loom.db`）で複数プロジェクトの Bead を扱いたいときは、[namespace](glossary.md#namespace)（名前空間）で分けます。

```
$ lm create --title "決済 API の再設計" --type task --priority 1 --namespace payments
- Created: payments-zgh [open/P1] 決済 API の再設計

$ lm list --namespace payments
Filter: namespace="payments", status="unfinished"
- payments-zgh [open/P1] 決済 API の再設計
```

`--namespace` を付けると、表示 ID の接頭辞がその namespace 名になります（既定は `id-`）。`lm list --namespace <名前>` を使えば、プロジェクトごとに絞り込めます。

### 別の端末と合流する

別の端末（または別のエージェント）で作業した分は、JSONL で書き出して手元の端末に取り込みます。ここでは、作業した側を端末 A、取り込む側を端末 B と呼びます。

まず端末 A で書き出します。

```
$ lm export > export.jsonl
```

書き出した `export.jsonl` を、`scp`・共有ストレージ・git など手近な方法で端末 B へ移します。続けて端末 B で取り込みます。

```
$ lm import export.jsonl
Imported: 3 bead(s), 0 dependency(ies), 0 token_cost(s)
Updated: 0 bead(s)
Dangling: 0 link(s)

## Newly ready
...
```

合流は列（フィールド）ごとに「最後に書いた値が勝つ」規則で行われるので、端末 A と端末 B の双方で同じ Bead を更新していても衝突エラーにはなりません。端末 B の変更も端末 A に取り込みたいときは、向きを逆にして同じ手順を繰り返します。取り込む前に件数だけ確かめたいときは、端末 B で `lm import --dry-run <file>` を使ってください。

### 実行者を用意しないとき

[実行者](glossary.md#実行者)（cron や CI のスケジュール実行で決まった間隔で起動し直されるエージェント）は Loom に含まれず、実装例も公開していません。自作するときの最小要件は用語集の「実行者」にあります。ここに書くのが Loom の既定の運用で、実行者を用意しなくてもこのとおりに使えます。実行者を使うときは、その実行者の文書が起動のしかた・`wait:` ラベルの拡張種別・通知の出し方・[受入](glossary.md#受入)と merge と [close](glossary.md#close) の担い手・PR の題名と本文の検査を上書きできます。上書きが無い項目は、次のとおり、あなたか[対話セッション](glossary.md#対話セッション)のエージェントが手で行います。

- `lm gate sweep` を、作業の区切り（セッションの始めなど）で実行します。塞いでいる Bead が全て終わった Gate を resolve します。
- `wait:` ラベルと `done:` ラベルは誰も測りません。`lm gate` で待ちの条件を読み、満たされたと確かめたら `lm gate resolve <id> --reason "<確かめた結果>"` で開けます。
- [claim](glossary.md#claim) は、止まったら自分で再開できるものだけが持ちます。対話セッションのエージェントに claim させたときは、セッションを閉じる前に `lm update <id> --release` で返すか `lm close` で閉じます。
- PR を受け入れて merge し、`lm close` で閉じるのも、あなたかエージェントが行います。

## 次に読むもの

- [docs/glossary.md](glossary.md) は用語集で、Loom で用いる中核概念と用語の定義、状態遷移、対応するコマンドを載せています。
- [skills/loom/SKILL.md](../skills/loom/SKILL.md) はエージェント向けの運用規約です。このチュートリアルが人間向けに「こういうときはこうする」をまとめたものであるのに対し、こちらはエージェントが従う手順そのものを書いています。
- [skills/loom/docs/gate.md](../skills/loom/docs/gate.md) は Gate の運用規約で、Gate の作成手順や種別（human / adjudicate / external / confirm）の使い分けを載せています。
- [README.md](../README.md) には、インストールと最初の使い方を載せています。
