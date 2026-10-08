# Loom

Loom は、AI コーディングエージェントが自分でタスクを取って進められるようにする、ローカルで動くタスクトラッカーです。

紹介ページ: <https://loombeading.github.io/>

## 用語

- [Bead](docs/glossary.md#bead): Loom が管理する作業・タスク・Gate・Epic の最小単位です。
- [依存](docs/glossary.md#依存): Bead 同士の先行・後行関係（blocks / parent-child）を表す有向リンクです。
- [Ready](docs/glossary.md#ready): 未着手（`open`）でブロッカーが無く、今すぐ着手できる Bead の状態です。
- [Gate](docs/glossary.md#gate): 自動進行を止め、人の承認や外部条件の成立を待つための特別な Bead です。
- [実行者](docs/glossary.md#実行者): cron や CI のスケジュール実行のように、決まった間隔で起動し直され、止まっても次の起動で作業を再開できる仕組みの上で、Bead を claim して作業を進めるエージェントです。実行者は Loom 本体には含まれず、利用者が用意します。自作するときの最小要件は [docs/glossary.md](docs/glossary.md#実行者) の「実行者」にまとめてあります。用意しないときの運用は [docs/tutorial.md](docs/tutorial.md#実行者を用意しないとき) の「実行者を用意しないとき」にあります。

その他の用語（claim, close, Newly ready, Gate の持ち主, 孤児 Gate, Epic, マイルストーン, course, wip, Compaction, namespace, token cost, rework, external-ref, --severity, --due, --expedite, 推論の深さ, redetect_key, revived_at, 係数, 設定時刻, 対話セッション, 受入, 判定者, 引き継ぎ簿, watcher, probe, statusline）は [docs/glossary.md](docs/glossary.md) を参照してください。

## インストール

経路は 4 つあり、どれも `lm version` で導入結果を確認できます。

最初の版はまだリリースしていません。(a)〜(c) はリリースのタグと Release を使うので、最初の版が [Releases](https://github.com/loombeading/loom/releases) に載るまでは使えません。それまでは (d) で導入してください。

### (a) Homebrew（最初のリリース後）

formula の実体 `Formula/lm.rb` は各版のリリース時にその版のチェックサムから生成してタグの木に入れるもので、最初の版を出すまではリポジトリにありません。

最初の版を出したあとは、macOS と Linux の Homebrew でこのリポジトリを tap として追加して入れます。formula は (c) と同じ Release のバイナリを、版ごとに固定した SHA-256 で照合してから配置します。

```sh
brew tap loombeading/loom https://github.com/loombeading/loom
brew install loombeading/loom/lm
```

版を上げるときは `brew upgrade loombeading/loom/lm` を実行します。

### (b) `go install`（最初のリリース後・clone せずにリリースのタグからビルド）

Go が入っていれば、これがいちばん手早い方法です。リポジトリを clone せず、Go のモジュールプロキシからタグの版のソースを取ってビルドします。

```sh
go install github.com/loombeading/loom/cmd/lm@latest
```

タグは CalVer（`v0.YYMM.N`）で、メジャー版を 0 に固定しているので、`@latest` がそのまま最新リリースを指します。`lm version` の `<版>` は Release のバイナリと同じタグになります。モジュールのソースには git の情報が含まれないので、`<コミット>` と `<日付>` は Release のバイナリと同じく `-` と表示されます。

### (c) GitHub Release から取得して検証する（最初のリリース後）

Release にはバイナリと SHA-256 のチェックサム一覧、Sigstore keyless 署名（GitHub artifact attestation）が同梱されています。ダウンロードしたら、チェックサムと attestation の両方を確かめてから配置してください。

repo 根の `install.sh` がこの手順をまとめて行います。`curl`・`gh`（GitHub CLI）・`sha256sum` か `shasum` が要り、macOS と Linux で動きます。どちらかの検証に失敗すると何も書かずに終了コード 1 で止まります。

`install.sh` は Release のアセットには含まれないので、リポジトリから取得します。clone するか、使う版のタグを指定して `install.sh` だけを取得してください。どちらも、実行する前に中身を読んでください。

```sh
# リポジトリを clone して repo 根で実行する
git clone https://github.com/loombeading/loom.git
cd loom
# または、タグを指定して install.sh だけを取得する（版を固定すると、読んだ中身と実行する中身が一致する）
tag=v0.YYMM.N  # Releases ページに載っている版のタグに置き換える
curl -fsSLO "https://raw.githubusercontent.com/loombeading/loom/${tag}/install.sh"
```

既定の置き場所は `~/bin/lm` です。`~/bin` が無ければ `install.sh` が作りますが、PATH への追加は行わないので、`lm` をコマンド名で呼べるように `~/bin` を PATH に入れておいてください（例: シェルの設定ファイルに `export PATH="$HOME/bin:$PATH"` を書く）。PATH の通ったほかの場所に置くなら `--dest` で指定します。

```sh
# 最新の Release を取得・検証して ~/bin/lm に置く
bash install.sh
# 版を指定する・置き場所を変える（tag は Releases ページに載っている版のタグ）
bash install.sh --release "$tag" --dest /usr/local/bin/lm
# Release を使わずソースからビルドして置く（go install）
bash install.sh --source
```

手で行う場合は次のとおりです。Release のバイナリは `lm_<OS>_<アーキテクチャ>` という名前で、次の 4 つがあります。

| OS | アーキテクチャ | アセット名 |
|---|---|---|
| macOS（Apple シリコン） | arm64 | `lm_darwin_arm64` |
| macOS（Intel） | amd64 | `lm_darwin_amd64` |
| Linux | amd64（x86_64） | `lm_linux_amd64` |
| Linux | arm64（aarch64） | `lm_linux_arm64` |

下の例は `uname` から自分の環境のアセット名を組み立てます。

```sh
# 0. 自分の環境のアセット名を決める
os=$(uname -s | tr '[:upper:]' '[:lower:]')        # darwin / linux
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
esac
asset="lm_${os}_${arch}"

# 1. バイナリとチェックサム一覧を取得する
curl -LO "https://github.com/loombeading/loom/releases/latest/download/${asset}"
curl -LO https://github.com/loombeading/loom/releases/latest/download/SHA256SUMS

# 2. チェックサムを照合する（不一致なら中断。Linux では sha256sum -c SHA256SUMS --ignore-missing でもよい）
shasum -a 256 -c SHA256SUMS --ignore-missing

# 3. Sigstore の attestation を検証する（gh: GitHub CLI）
gh attestation verify "$asset" --repo loombeading/loom

# 4. 検証を通ったら配置する（~/bin が無ければ作る。~/bin を PATH に入れておく）
mkdir -p "$HOME/bin"
chmod +x "$asset"
mv "$asset" "$HOME/bin/lm"
```

### (d) clone したリポジトリからビルド（いま使える経路）

リポジトリを clone し、その根で次を実行します。

```sh
git clone https://github.com/loombeading/loom.git
cd loom
go install ./cmd/lm
```

`$GOBIN`（未設定なら `$GOPATH/bin`）にビルドして配置します。`lm version` の `<版>` は直近のタグから Go が作る擬似版（作業木に変更があれば末尾に `+dirty`）になり、`<コミット>` と `<日付>` には checkout しているコミットが入ります。

ソースを変更して確かめる開発者向けの手順は、導入とは別に次のとおりです（Release と同じく cgo を使わずにビルドし、全テストを走らせます）。

```sh
CGO_ENABLED=0 go build -trimpath ./cmd/lm
go test ./...
```

### 導入確認

```sh
$ lm version
lm v0.YYMM.N (- -)
```

括弧の中はビルド元のコミット（先頭 9 桁）とその日付で、版とともに導入したものに応じて変わります。(b) の `go install ...@latest` と (a)・(c) の Release のバイナリはコミットの情報を埋め込まないので、どれも `-` になります。コミットは版のタグから辿れます。(d) の clone したリポジトリからのビルドでは、checkout しているコミットとその日付が入ります（例: `(6ebd72c31 2026-10-01)`）。

### 版を上げたとき

`lm` の新しい版は、既存の `loom.db` のスキーマ（表の構造）を新しい形に移す必要があることがあります。DB のスキーマ版がその版の期待より古いと、`lm` は DB を開いた時点で `database schema is older than this build of lm supports; run "lm migrate"` というエラーで止まります。版を上げたら、使い始める前に次の 2 つを実行します。

```sh
$ lm where
- Database: /tmp/tut/.loom/loom.db
- Resolved via: LM_DIR
- Config: /home/you/.config/loom/config.json (not found)
- Schema: 17 (binary expects 17)
$ lm migrate
- Schema: 17 (already current)
```

`lm where` は使っているデータベースの場所と、`Schema:` 行に DB のスキーマ版とバイナリが期待する版を出します。2 つの数が違うときは `lm migrate` で DB のスキーマをバイナリの期待する版まで進めます。`lm export`・`lm import` での作り直しは監査ログ（変更履歴）を失うので、移行には使いません。

## 5 分で使う

リポジトリで `lm` を使い始め、タスクを 1 件作って着手し、閉じるまでの流れです。下の出力は、一時ディレクトリ `/tmp/tut` で実行した実際の出力です。

`lm init` は `LM_DIR` が指す場所にデータベースを作ります（`LM_DIR` が未設定のときの置き場所は次の段落）。`@alice` は claim した主体の名前で、`LM_ACTOR` から取ります（コマンドごとに `--actor` で上書きできます）。

`lm` はデータベース（`loom.db`）をデータディレクトリに置きます。環境変数 `LM_DIR` はそのデータディレクトリの場所で、絶対パスで指定します（例: `LM_DIR=/tmp/tut/.loom`。相対パスはエラーになります）。`LM_DIR` が未設定のときは `$XDG_DATA_HOME/loom`（`XDG_DATA_HOME` も未設定なら `~/.local/share/loom`）を使い、カレントディレクトリや親ディレクトリの `.loom` は見ません。解決先に `loom.db` が無ければ `lm init` 以外のコマンドはエラーになります。今どこを使っているかは `lm where` で確かめられます。詳しくは [skills/loom/docs/env.md](skills/loom/docs/env.md)（[GitHub 上の版](https://github.com/loombeading/loom/blob/main/skills/loom/docs/env.md)）を参照してください。

```
$ export LM_DIR=/tmp/tut/.loom LM_ACTOR=alice

$ lm init
- Database: /tmp/tut/.loom/loom.db

$ lm create --title "認証まわりのバグ修正" --priority 1
- Created: id-8a5 [open/P1] 認証まわりのバグ修正

$ lm ready --claim
Claimed: id-8a5
- id-8a5 [in_progress/P1] 認証まわりのバグ修正  @alice

$ lm close id-8a5 --reason "修正して確認済み"
- Closed: id-8a5
## Newly ready
(none)
```

Bead の行の角括弧は `[type/status/P<n>]` の順に並びます。type は既定の `task` のときだけ省いて `[status/P<n>]` になり（`[open/P1]` は task の Bead）、`--external-ref` に PR の URL があると status の位置に `#<PR 番号>` が出ます。

`lm close` は、閉じたことで新たに Ready になった Bead を `## Newly ready` に出します。`--summary`（要約）と `--reason`（理由）はどちらも任意です。`--summary` を付けると、閉じたあとの `lm show` で本文の代わりに要約が出ます（[docs/glossary.md](docs/glossary.md) の close）。Gate で人の判断を仰ぐ、眠る前に確認する、仕事を積むといった続きは [docs/tutorial.md](docs/tutorial.md) を参照してください。

## ヘルプと補完

コマンド一覧は `lm help`、個別コマンドの説明は `lm help <command>`（`lm <command> --help` と同じ）で確認できます。どちらもデータベースを開かないので、`LM_DIR` が未設定でも動きます。シェル補完は次のように有効にします。

- zsh: `~/.zshrc` に `eval "$(lm completion zsh)"` を追加します。
- bash: `~/.bashrc` に `eval "$(lm completion bash)"` を追加します。

## エージェントで使う

`lm` の出力は Markdown だけで、どのコマンドも実行中に入力を待たずに完結します。エージェントに `lm` を使わせるときは、推奨ルール（loom skill）を利用者のリポジトリに入れてください。GitHub CLI で次のように入れると、`--agent` に指定したエージェントが読む場所に置かれます。

```sh
gh skill install loombeading/loom loom --agent <エージェント>
```

`gh skill` は GitHub CLI 2.90.0 で追加されたコマンドです（Public Preview）。`gh --version` が 2.90.0 より古いときは GitHub CLI を更新してください。`<エージェント>` には、たとえば次の値を指定します。

- Claude Code: `claude-code`
- Codex: `codex`
- Gemini CLI: `gemini-cli`
- Cursor: `cursor`
- GitHub Copilot: `github-copilot`（`--agent` を省いたときの既定）

指定できる値の全一覧は `gh skill install --help` の「Supported `--agent` values」に出ます。`--scope user` を付けるとリポジトリではなくホームディレクトリに入り、どのリポジトリでも読まれます。

`gh skill` を使えない場合は、このリポジトリの `skills/loom` ディレクトリを丸ごと、エージェントが skill を読むディレクトリ（Codex・Gemini CLI・Cursor なら `.agents/skills/loom`、Claude Code なら [Claude Code の Skills の文書](https://docs.claude.com/en/docs/claude-code/skills) にあるプロジェクト skill の置き場所）へコピーしてください。

## 関連文書

- [docs/tutorial.md](docs/tutorial.md) — チュートリアルです。Gate への回答、眠る前の確認、仕事の積み方、複数プロジェクト・複数端末の運用など、人が日常的に行うことを説明しています。
- [docs/glossary.md](docs/glossary.md) — 用語集です。Loom の中核となる概念と用語の定義、状態遷移、対応するコマンドをまとめています。
- [docs/prd-harness.md](docs/prd-harness.md) — Loom とは別の製品で、Loom の計画をエージェントに実行させる[実行者](docs/glossary.md#実行者)の一つの要件文書です。この実行者の実装はこのリポジトリに含まれません。Loom を使うために読む必要はありません。
- [docs/prd-planner.md](docs/prd-planner.md) — Loom とは別の製品で、計画をグラフとして組み立てるプランナーの要件文書です。このプランナーの実装はこのリポジトリに含まれません。Loom を使うために読む必要はありません。
- [Releases](https://github.com/loombeading/loom/releases) — 変更履歴です。
- [SECURITY.md](SECURITY.md) — セキュリティポリシーと脆弱性の報告先です。
- [CONTRIBUTING.md](CONTRIBUTING.md) — 不具合の報告の送り方を説明しています。

## ライセンス

Loom は Apache License 2.0 で提供しています。

- [LICENSE](LICENSE) — Loom 本体のライセンス全文です。
- [NOTICE](NOTICE) — 著作権表示と帰属表示です。再配布するときは同梱してください。
- [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) — バイナリに組み込まれる依存モジュールのライセンス全文です。
