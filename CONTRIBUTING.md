# Contributing

Loom に関心を持っていただき、ありがとうございます。このリポジトリは、不具合の報告と脆弱性の非公開の報告を受け付けています。

## 不具合の報告

[Issue](https://github.com/loombeading/loom/issues/new/choose) を開き、「不具合の報告」のフォームを選んで書いてください。届いた報告にはすべて目を通します。フォームは次の項目を尋ねます。

- lm version（`lm version` の出力）
- OS と CPU
- 入れ方
- 再現手順
- 期待した結果
- 実際の結果と終了コード
- 再現データ（任意）

再現データには、`lm export` の出力から再現に要る行だけを残して貼ってください。`lm export` はタスクのタイトル・本文・変更理由をそのまま書き出すので、機密や個人情報を含む行は貼らずに除くか、内容を伏せた文字列に置き換えてください。

## Pull Request

このリポジトリは、リリースごとに開発の成果をまとめて公開しています。ここに届いた Pull Request は取り込みません。そのため、貢献者に DCO（Developer Certificate of Origin・出所の証明）の署名や CLA（Contributor License Agreement・貢献者ライセンス契約）の締結は求めません。

## 要件文書

`docs/prd-harness.md` と `docs/prd-planner.md` は、Loom とは別の製品の要件文書です。どちらの製品も実装はこのリポジトリに含まれず、オープンソースソフトウェアとして提供していません。

## 脆弱性の報告

セキュリティ上の問題は公開の Issue に書かず、[SECURITY.md](SECURITY.md) の手順で非公開のまま知らせてください。
