## PR 運用

- PR を作成したら `lm update <id> --external-ref <PR URL>` で記録する。`--external-ref` は追加であり、付け替えではなく履歴として残る（複数回実行しても前の URL は消えない）。消すときは `lm update <id>
  --remove-external-ref <URL>` を使う。記録すると `lm list` 等の列挙の角括弧内に、記録した URL の中で末尾から見て最初に一致した GitHub PR の `#<n>` が出るようになり、PR 番号が一覧から見える。未完了（open・in_progress）なら状態の位置を `#<n>` に置き換え（例: `- id-8a5 [#42/P1] 認証まわりのバグ修正  @alice`）、closed・cancelled になった後は状態を残したまま `#<n>` を並記する（例: `- id-8a5 [closed/#42/P1] 認証まわりのバグ修正`）——closed・cancelled でも PR 番号は出るが、open と区別できなくなるので状態は消さない。GitHub の PR URL 以外の値は行に出ない（`lm show` で確認する）。
- `lm ahead` の見積もりを使うなら、PR を作成したときに `lm cost add <id> --in <n> --out <n> --reason "PR <URL>"` でその作業の消費トークン（`--in` は入力、`--out` は出力のトークン数）を記録する。記録した値が `lm ahead` の見積もりの元になる（[用語集](https://github.com/loombeading/loom/blob/main/docs/glossary.md)「token cost」）。見積もりを使わない運用では記録しなくてよい。
- 差し戻しの率を見たいなら、PR が CI で差し戻された・レビュー指摘で追いコミットしたときに `lm rework add <id> --cause <ci|review|followup> --reason "PR <URL>"` で 1 回につき 1 件記録する（`lm` は GitHub を読めないので、差し戻しは記録しない限り観測できない）。記録は `lm ahead --calibration` の `## Rework` 節に depth 別の差し戻し率として出る（[用語集](https://github.com/loombeading/loom/blob/main/docs/glossary.md)「rework」）。
- CI を待つ実行者は、CI 失敗を検知したら `lm rework add <id> --cause ci` を自動で記録してよい。同じ head で二重に記録しないよう、修正を push してから次を待つ。
- `lm ahead [--budget <n>]` は、Ready な Bead と後続の段（`blocks`が外れて Ready になる Bead）の消費見込みを closed Bead の実測中央値から算出する（読み取り専用・永続化しない派生値）。`--budget` を渡すと`## Summary` の `Budget:` 行に、Ready を引いた残りが Ready・各段のどこまでをまかなえるか（`covers through Course <k>` 等）が出る。着手前に今夜の予算で何段まで捌けるかの見積りに使う。`## Milestones` 行の `gate=` は human 型の Gate に塞がれた子孫だけを数える（型の判定は `lm gate` と同じ。`lm ahead` の `--all` で全型）。
- PR が merge されたら `lm close <id> --reason "PR <URL> merge"` でクローズする。
- PR の題名・本文は、PR を出す repo の規約（CONTRIBUTING.md 等）に従って書く。Loom は題名と本文の書式を定めず、既定では作成前に検査しない。実行者の文書が検査を足していれば、それに従う。
- 複数環境で作成された Bead を合流させるには、合流元で `lm export > <path>` し、合流先で `lm import <path>` する。先に `lm import --dry-run <path>` で結果を確かめられる。
- 自律的な提案（人間の判断が必要な変更）は `lm gate create --kind adjudicate` で Gate を作り、作業対象の Bead を `blocks` で塞ぐ。判定者が人の操作が要ると結論したら、`--kind human --adjudicated-by <判定 Gate>` で人の Gate を作る（SKILL.md 手順 4）。人間は `lm gate` を見て`lm gate resolve <gate-id>`（go）か `lm gate reject <gate-id>`（やらない。塞いでいる Bead と Gate をまとめて cancelled にする）で判断する（エージェントとの対話で行ってよい）。
