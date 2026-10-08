# Product Requirements Document (PRD): ハーネス

本文書は参考資料であり、Loom の利用者の必須知識ではない。本文書は Loom には含まれない別製品の要件で、その製品の実装は公開していない。Loom を使うために本文書を読む必要は無く、Loom 単体で計画の管理と実行の記録は完結する。本文書は、Loom の計画を実行する側に何を期待できるかを示すだけで、本文書の用語（関門・Agent Guard など）と末尾の Back Matter は本文書の中だけで使い、Loom の機能や使い方を定めない。本文書を単体で参照するときは、参考 PRD として扱う。

## この文書の範囲

本文書は本ハーネスが Loom の利用者に対して**保証すべき性質**（何を・誰に・なぜ）を書く。それを実現する機構（設計・手順・実装の詳細）は書かない。計画を保存する側（Loom）と計画を組み立てる側の性質は、それぞれの文書が持つ。

参照書式: 節は `N.M`、章全体は `N章` で参照する。

## 1. プロダクト概要 (Product Overview)

- **プロダクト名**: 本文書では「本ハーネス」と呼ぶ
- **コンセプト**: Loom に書かれた計画を、AI コーディングエージェントに並列で実行させ、結果を Loom に書き戻すハーネス。
- **目的**: 人が見張らなくても、計画の Ready な作業が前に進み、止まったものは自分で回復する状態を保つ。

### 1.1 用語 (Terminology)

- **ハーネス (Harness)**: エージェント本体の外側にあって、エージェントを起動し、行動を制約し、結果を記録する仕組みの総称。本文書では、Loom の計画を実行し結果を Loom に書き戻すハーネスを「本ハーネス」と呼ぶ。
- **計画**: Loom に保存された Bead とリンクのグラフ。本ハーネスは計画を読み、実行の結果（状態・理由・PR）を書き戻す。計画の保存は Loom、計画の組み立ては別の製品の担当である（4.3）。
- **エージェントのセッション**: 本ハーネスが非対話で起動する、エージェントの 1 回の実行。1 件の Bead を前に進めて終わる（5.1）。セッションを動かす Claude Code などのプログラムは、本文書では「エージェント CLI」と呼ぶ（5.6）。
- **用語集との対応**: Loom の[用語集](glossary.md)の「実行者」（Bead を claim して進める主体）は本ハーネスにあたり、claim を持つのは本ハーネスで、本ハーネスが起動したセッションは claim しない。用語集の「対話セッション」は本ハーネスの外にあり、claim しない（5.1）。
- **関門 (Gate Check)**: エージェントの操作の直前に走り、規約を満たさない操作を**拒否**する検査（5.2）。Loom の Gate（待機のプリミティブ）とは別物である。
- **引き継ぎ簿**: Bead ごとに進捗と判断を書き残すファイル。セッションが途中で終わっても、次のセッションが続きから再開できる（5.3）。
- **受入**: エージェントが作った PR が計画どおりであることを確かめ、merge へ進めてよいと記録すること。この記録は merge を許す唯一の肯定の合図である（5.4）。
- **判定者**: 設計の分岐や取り消せない操作の可否を決める役割のエージェント（5.5）。
- **fail-closed（拒否側へ倒す）**: 関門が判定に至らなかったとき（落ちる・読めない・時間切れ）に、通過ではなく拒否として扱う性質（4.2）。

## 2. 背景と課題 (Background & Problem)

### 2.1 計画は書いても、実行する者がいない

Loom は着手可能な作業（Ready）を正確に示すが、それを拾って進める主体は持たない。対話セッションは利用者が閉じれば終わり、閉じた瞬間に着手中の作業は誰にも再開されなくなる。

### 2.2 無人の実行は、止まったことに誰も気付かない

エージェントのセッションは黙って止まることがある。人が様子を見に来るまで止まったままになり、その間の時間は取り戻せない。

### 2.3 エージェントは規約を読んでも守りきれない

文書に書いた規約は読み飛ばされ、同じ誤りが繰り返される。規約を守らせるには、違反する操作をその場で止める仕組みが要る。

### 2.4 関門は到達しないことで破れる

検査は判定の誤りより先に、**検査自体が走らない**ことで破れる。検査が落ちる・入力が読めない・時間切れで強制終了される、のいずれも「決定なし」となり、決定なしは通過と区別がつかない。

## 3. ターゲットユーザー (Target Audience)

- **一次ユーザー（利用者）**: Loom で計画を管理し、その実行を AI コーディングエージェントに任せたい個人開発者。計画を書き、人にしかできない判断（公開・取り消せない操作）だけを受け持つ。
- **エージェント**: 本ハーネスに起動され、本ハーネスの関門に止められる側。関門の拒否理由を読んで行動を直す。

## 4. コアバリューと解決策 (Core Value & Solution)

### 4.1 止まったものを自分で回復する (Self-recovering Execution)

異常は人より先に本ハーネスが検知する。検知は「進んでいるように見えるか」ではなく、成果（close・PR の作成と更新）が一定時間出ていないことで行う。既知の原因なら回復して作業を再開し、何をしたかを計画に記録する。未知の原因なら、計画に作業として起票し、次からは既知の原因として回復する。同じ原因は二度起票しない。基準: 重複した起票は計画の流入を膨らませる。

### 4.2 到達しない経路を拒否側へ倒す (Fail-closed by Construction)

本ハーネスのすべての関門は、判定に至らなかった 3 経路（異常終了・入力や設定の読み取り失敗・時間切れ）を拒否として扱う。関門が通過を返すのは、判定が明示的に通過と決めたときだけである。

### 4.3 Loom とエージェントから独立した層 (Independent Layer)

本ハーネスは計画を保存しない（Loom の担当）し、計画を組み立てない（別の製品の担当）。Loom とエージェント CLI を外部プロセスとして呼び、実行時の判断だけを受け持つ。

### 4.4 監査可能性 (Auditable Decisions)

起動の判断・関門の拒否・回復の操作は、Loom の変更理由と記録に残る。「なぜ起動したか」「なぜ止めたか」を後から人とエージェントが追える。

## 5. 主要機能要件 (Key Functional Requirements)

### 5.1 計画の並列実行 (Scheduler)

- 本ハーネスは Loom の Ready な Bead を claim し、エージェントのセッションを非対話で起動して進める。claim するのは本ハーネスだけで、セッションと対話セッションは claim しない。
- 同じ Bead を 2 つのセッションが同時に進めることはない。複数のセッションは互いに別の Bead を取る。
- 本ハーネスはセッションにあてる Bead を Loom の `lm ready` の並びの順に取り、自前で並べ替えない。基準: 並びを Loom の外でも計算すると、`lm ready`・`lm ahead`・`lm gate` が示す順と実際に拾われる順が食い違う。
- 実効優先度 P0 の Bead（割り込み（expedite）が有効なものと、それを塞ぐ前提）は、他の Ready より先に起動する。走行中のセッションは止めない。
- どの Bead をいつどのセッションにあてるか（実行時の優先度の組み替え）は本ハーネスが受け持つ。期間をまたぐ配分と順序は計画を組み立てる側が受け持つ。
- 期限（`due_at`）を持つ Bead が期限までに終わらない見込みになったら、本ハーネスは理由を添えてその鎖の根に割り込みを付けることがある。期限そのものは計画を組み立てる側か利用者が付け、本ハーネスは付けない。
- 同じ作業を前進なしに拾い直し続けるときは、人か判定者の判断待ちとして Loom の Gate で塞ぎ、次の作業へ進む。

### 5.2 エージェントへの関門 (Agent Guards)

- エージェントの操作の直前に走り、規約に反する操作（main への直接の変更、許された範囲の外への書き込み、引き継ぎ簿を伴わない委譲、信頼できない入力を実行する形のコマンドなど）を拒否する。
- 拒否理由は、エージェントが読んで行動を直せる文で返す。
- すべての関門は 4.2 の fail-closed に従う。

### 5.3 中断に強い委譲 (Resumable Delegation)

- エージェントの作業状態を、エージェントの文脈ではなく Bead ごとの引き継ぎ簿に置く。セッションがどこで終わっても、次のセッションは簿を読んで続きから再開し、終わった手順をやり直さない。
- 簿に完了の印が無い作業は未完了として扱う。エージェントの報告だけでは完了とみなさない。

### 5.4 PR の受入と後片付け (Acceptance & Reconcile)

- エージェントが作った PR は、CI の結果と計画との一致を確かめる受入を経てから merge へ進む。CI が通っただけでは merge しない。
- 受入の記録は、計画（Bead が求めたこと）と差分が一致することを実測で確かめた記録である。本ハーネスでは、受入を終えた PR に本ハーネスの受入のセッションが `accepted` ラベル（[用語集](glossary.md)「受入」）を付けることを記録とし、作業したセッション自身は付けない。本ハーネスの実装は公開していないので、同じ運用を再現する利用者は自分の実行者で次の 2 つを用意する。1 つは、PR の差分とテスト結果を Bead の求めと突き合わせ終えた受入の担い手（作業した担い手とは別）だけが `accepted` を付ける手順である。もう 1 つは、merge の直前に `accepted` が付いていることを確かめ、付いていない PR を merge しない検査である。Loom は `accepted` を保存も解釈もしない。
- 本ハーネスは merge を、受入の記録と現在状態（check がすべて success・最新の main と合わせた build・test が緑）だけで決める。レビューの承認は意見であり、merge の条件に入らない。基準: 計画との一致を確かめた記録が無いまま merge できると、CI が測らない食い違い（求められていない変更・求めの取りこぼし）がそのまま main に入る。
- 受入の記録が無い PR は merge しない。記録の欠けは待つだけで、誤 merge にはならない。
- マイルストーンの子 Bead が出荷条件を満たす変更の PR を受け入れるとき、受入は親のマイルストーン Epic にその出荷条件に当たる `hold:` ラベルがあることを確かめ、無ければ受け入れない。Epic に `hold:` が 1 件以上あるかは受入の機械検査が測り、変更と `hold:` の対応は受入の読み手が判断する。崩れうる出荷条件に当たる `hold:` が無いと分かったときは、`hold:` とその測り方を足す Bead を起票する。基準: 出荷条件を満たした変更を `hold:` 無しで受け入れると、後の変更で崩れても誰も測らない。
- 本ハーネスは、merge された作業の Bead を閉じ、使い終えた作業場所を片付け、止まった作業の claim を解放する（後片付け）。

### 5.5 判断の振り分け (Judgment Routing)

- 規約で流れが決まっている操作は、人に聞かずに本ハーネスとエージェントが進める。
- 設計の分岐と取り消せない操作の可否は判定者に振り、結論を計画に記録してから進める。
- 人に回すのは、人にしかできない操作（公開・外部への送信・履歴の書き換え）だけである。人の判断待ちは Loom の Gate で表し、判断が記録されれば本ハーネスが作業を再開する。
- コマンドで測れる待ちの Gate（Loom の `wait:` ラベル）は、本ハーネスが測って開け、実測値を理由に記録する。測れないときは開けない。
- 本ハーネスは、マイルストーンの出荷条件（Loom の `hold:` ラベル）を `done:` と同じ測り方で毎サイクル測る。崩れていれば（測れない・知らない種別も崩れ側に数える）是正の Bead を同じ Epic とラベルにつき 1 件だけ起票し、`hold-guard` を持つ Bead をそれで塞ぎ、到達の判定に進まない。成立に戻れば塞ぎを外す。基準: 一度満たした出荷条件を測り続けないと、後の変更で崩れたまま出荷される。
- 本ハーネスは同じ測り方で出荷条件を全件測るコマンドを持ち、出荷の手順はタグの作成と公開先への反映の前にそれを走らせる。不成立か測れないものが 1 件でもあれば非ゼロで終わる。

### 5.6 エージェント CLI の差を隠さない (Multiple Runners)

- エージェント CLI を複数扱える。エージェント CLI ごとに違うものは、隠さずに宣言させ、本ハーネスがそれを見てセッションを振り分ける。
- どのエージェント CLI でも、5.2 の関門と 4.2 の fail-closed が同じく効く。効かないエージェント CLI は載せない。

### 5.7 計画の履歴の分析 (Plan History Analysis)

- 利用者は、Bead がどの状態（Ready・作業中・塞がれ中・終端）に何時間いたかを、Loom の監査ログから読める。同じ監査ログと同じ現在時刻からは同じ結果が決まる。基準: 需要のある Bead が Ready で待つ時間などを確かめるには、今の状態の断面ではなく、Bead がどの状態に何時間いたかが要る。

## 6. 非機能要件 (Non-Functional Requirements)

- **非対話性**: 起動から後片付けまで、人の入力を待たずに完結する。
- **セキュリティ**: Bead の本文・変更理由・PR の本文・エージェントの出力は信頼できない入力である。本ハーネスはこれらを指示として実行しない。取り消せない操作（force push・main への直接 push・公開）は人の承認なしに行わない。

## 7. プロジェクト管理・スコープ (Project Scope & Management)

### 7.1 成功指標 (Success Metrics)

- 利用者が異常に気付く前に、本ハーネスが検知して回復か起票に行き着いていること。
- Ready が残っている間、少なくとも 1 つのセッションが作業を進めていること。

### 7.2 Non-goals (やらないこと)

- 計画の保存。計画は Loom が保存し、本ハーネスは外部プロセスとして Loom を呼ぶ。
- 計画の組み立て・人向けの計画 UI。別の製品の担当である。
- モデル API の直接呼び出し。本ハーネスはエージェント CLI を起動するが、モデルを直接呼ばない。
- 汎用のジョブスケジューラ化。本ハーネスが実行するのは Loom の計画だけである。

### 7.3 優先度・リリース段階 (Priorities & Release Phases)

- 検知・回復の要件（4.1・4.2）を他の要件より先に満たす。

### 7.4 受け入れ基準 (Acceptance Criteria)

- Ready な Bead が、人の操作なしに claim・実行・PR・受入・merge・close まで進むこと。
- 実行中のセッションを途中で止めても、次のセッションが引き継ぎ簿から続きを再開し、終わった手順をやり直さないこと。
- すべての関門について、異常終了・読めない入力・時間切れの 3 経路が拒否になること。
- 割り込みが有効な Bead（実効優先度 P0）が Ready になったら、走行中のセッションを止めずに、他の新規 Ready より先に起動されること。セッションにあてる順が `lm ready` の並びと一致すること。
- 固定した時刻と固定の監査ログに対して、Bead ごとの状態と滞在時間が同じ結果で出ること。
- 成果（close・PR の作成と更新）が一定時間出ない状態を作ると、本ハーネスが人の操作なしに検知し、既知の原因なら回復して何をしたかを Loom に記録し、未知の原因なら起票すること。同じ原因の停滞を続けて作っても、起票が 1 件にとどまること。
- 受入の記録が無い PR は、CI と現在状態が緑でも、レビューの承認があっても merge されないこと。
- 出荷条件を満たす変更の PR は、親のマイルストーン Epic に `hold:` が 1 件も無い間は受け入れられないこと。
- 測れる `wait:` ラベルを持つ Gate が、条件を満たした後に開き、実測値が理由に残ること。測れない Gate は開かないこと。
- `done:` を満たしたマイルストーンでも、`hold:` が崩れているか測れない間は到達の判定に進まず、是正の Bead が 1 件だけ起票されて `hold-guard` の Bead が塞がれること。
- 新しいエージェント CLI を載せるとき、5.2 の関門と 4.2 の 3 経路の拒否が同じく効くことを確かめ、効かなければ載せないこと。
- 関門の拒否理由に、反した規約と、代わりにとれる操作が書かれていること。

---

本文の各要件は、下の Back Matter のいずれかの行に当たる。

# Back Matter

- The following sections represent a pure knowledge graph, stripped of narrative.

## Entities

### Classes

- Agent Harness: エージェント本体の外側で起動・制約・記録を行う仕組みの集合。
- Gate Check: 操作の直前に走り条件未達なら拒否する検査の集合。
- Agent CLI: 非対話で起動できるエージェントのコマンドの集合。
- Planner: 計画を組み立てる製品の集合。
- Untrusted Input: 本ハーネスが指示として実行してはならない入力の集合。
- Failure Path: 関門が判定に至らない経路の集合。

### Entities

- Harness: 本製品。
- Agent Session: 本ハーネスが非対話で起動する、エージェントの 1 回の実行。
- Runner: Agent Session を動かすエージェント CLI の 1 つ。
- Agent Guard: 本ハーネスが持つ関門。
- Loom: 計画を保存するトラッカー。
- Graph Planner: 計画を組み立てる製品。
- Plan: Loom に保存された Bead とリンクのグラフ。
- Bead: 計画の作業 1 件。
- Loom Gate: 人か判定者の判断待ちを表す Loom の Gate。
- Wait Label: コマンドで測れる待ちを表す Loom の `wait:` ラベル。
- Done Label: マイルストーンの到達を表す Loom の `done:` ラベル。
- Hold Label: マイルストーンが到達まで保つ出荷条件を表す Loom の `hold:` ラベル。
- Hold Guard: Hold Label が崩れている間に塞がれる、`hold-guard` ラベルを持つ Bead。
- Remedy Bead: 崩れた Hold Label を直すために起票される Bead。
- Handoff Note: Bead ごとの引き継ぎ簿。
- Pull Request: Agent Session が作る PR。
- Adjudicator: 判定者の役割のエージェント。
- Human Maintainer: 利用者。
- Interactive Session: 利用者が開く対話のセッション。
- Irreversible Operation: 人にしかできない操作（force push・main への直接 push・公開・外部への送信・履歴の書き換え）。
- Failure Cause: 本ハーネスが検知した異常の原因。
- Runtime Reprioritization: 実行時の優先度の組み替え（どの Bead をいつどの Agent Session にあてるか）。
- Long-range Allocation: 期間をまたぐ配分と順序。
- Process Crash: 異常終了の経路。
- Unreadable Input: 入力・設定の読み取り失敗の経路。
- Budget Exceeded: 時間切れの経路。
- LLM Invocation: モデル API の直接呼び出し。
- Bead Text: Bead の本文。
- Change Reason: Loom の変更理由。
- PR Body: PR の本文。
- Agent Output: Agent Session の出力。
- Audit Log: Loom が記録する状態の履歴。
- State Interval: Bead が 1 つの状態（Ready・作業中・塞がれ中・終端）にいた区間。
- Ready Order: Loom の `lm ready` が示す Ready の並び。
- Expedite: Loom の割り込み。有効な間は Bead を実効優先度 P0 にする。
- Due Date: Bead の期限（`due_at`）。
- Acceptance Mark: 受入の記録。計画と差分の一致を実測で確かめた記録。
- Review Approval: レビューの承認。
- Rule-defined Operation: 規約で流れが決まっている操作。

### Events

- Launch: 本ハーネスが Agent Session を 1 回起動する事象。
- P0 Launch: 割り込み（expedite）が有効で実効優先度 P0 の Bead の launch。
- Regular Launch: P0 以外の新規 Ready の launch。
- Stall Detection: 成果が一定時間出ていないことを本ハーネスが見つける事象。
- Recovery: 検知した異常から作業を再開する事象。
- Filing: 未知の原因を計画に起票する事象。
- Escalation: 前進なしの拾い直しが続き、Loom Gate で塞ぐ事象。
- Agent Operation: Agent Session がツールを操作する事象。
- Rejection: 関門が操作を拒否する事象。
- CI Pass: PR の CI が通る事象。
- Acceptance: PR が受入を経る事象。
- Merge: PR が merge される事象。
- Reconcile: merge された Bead を閉じ、作業場所を片付け、止まった claim を解放する事象。
- Due Risk: 期限つきの Bead が期限までに終わらない見込みになる事象。
- Duplicate Filing: 同じ原因を二度起票する事象。

### States

- Passed: 関門が明示的に通過と決めた状態。
- Rejected: 関門が拒否した状態。
- Undecided: 関門が判定に至らなかった状態。Rejected として扱う。
- Claimed: Bead が claim された状態。
- Resolved: Loom Gate に判断が記録された状態。
- Known Cause: 回復の手が分かっている原因の状態。
- Unknown Cause: まだ分類に無い原因の状態。
- Broken: Hold Label が不成立か測れない状態。

## Monosemantic Synapses

### Product Identity

- Harness -[is an instance of]-> Agent Harness
- Harness -[has part]-> Agent Guard
- Agent Guard -[is an instance of]-> Gate Check
- Harness -[executes]-> Plan
- Loom -[stores]-> Plan
- Plan -[has part]-> Bead
- Graph Planner -[is an instance of]-> Planner
- Graph Planner -[composes]-> Plan
- (Harness -[stores]-> Plan) -[does not hold]
- (Harness -[composes]-> Plan) -[does not hold]
- (Harness -[performs]-> LLM Invocation) -[does not hold]

### Execution

- Harness -[launches]-> Agent Session
- Agent Session -[runs on]-> Runner
- Runner -[is an instance of]-> Agent CLI
- Agent Session -[reads]-> Handoff Note
- Agent Session -[writes]-> Handoff Note
- Agent Session -[creates]-> Pull Request
- Harness -[claims {resulting state: Claimed}]-> Bead
- (Agent Session -[claims]-> Bead) -[does not hold]
- (Interactive Session -[claims]-> Bead) -[does not hold]
- Harness -[performs]-> Runtime Reprioritization
- (Harness -[performs]-> Long-range Allocation) -[does not hold]
- Graph Planner -[performs]-> Long-range Allocation
- P0 Launch -[precedes]-> Regular Launch
- ((Harness -[stops]-> Agent Session) -[holds when]-> P0 Launch) -[does not hold]
- (Harness -[opens]-> Loom Gate) -[holds when]-> Escalation
- Harness -[follows]-> Ready Order
- (Harness -[reorders]-> Ready Order) -[does not hold]
- Expedite -[causes]-> P0 Launch
- (Harness -[attaches]-> Expedite) -[holds when]-> Due Risk
- (Harness -[sets]-> Due Date) -[does not hold]
- Graph Planner -[sets]-> Due Date
- Human Maintainer -[sets]-> Due Date

### Self-recovery

- Harness -[has attribute]-> "停滞は成果（close・PR の作成と更新）の途絶で判定する"
- (Stall Detection -[causes]-> Recovery) -[holds when]-> (Failure Cause -[becomes]-> Known Cause)
- (Stall Detection -[causes]-> Filing) -[holds when]-> (Failure Cause -[becomes]-> Unknown Cause)
- Harness -[classifies {resulting state: Known Cause}]-> Failure Cause
- Harness -[records {to: Loom}]-> Change Reason
- (Harness -[performs]-> Duplicate Filing) -[does not hold]

### Fail-closed

- Process Crash -[is an instance of]-> Failure Path
- Unreadable Input -[is an instance of]-> Failure Path
- Budget Exceeded -[is an instance of]-> Failure Path
- (Agent Guard -[becomes]-> Undecided) -[holds when]-> (Agent Guard -[falls into]-> Process Crash)
- (Agent Guard -[becomes]-> Undecided) -[holds when]-> (Agent Guard -[falls into]-> Unreadable Input)
- (Agent Guard -[becomes]-> Undecided) -[holds when]-> (Agent Guard -[falls into]-> Budget Exceeded)
- (Agent Guard -[becomes]-> Rejected) -[holds when]-> (Agent Guard -[becomes]-> Undecided)
- ((Agent Guard -[becomes]-> Passed) -[holds when]-> (Agent Guard -[becomes]-> Undecided)) -[does not hold]
- (Agent Guard -[becomes]-> Rejected) -[causes]-> Rejection
- Rejection -[prevents]-> Agent Operation
- Agent Guard -[governs]-> Runner
- (Harness -[uses]-> Runner) -[holds when]-> (Agent Guard -[governs]-> Runner)

### Acceptance & Reconcile

- Acceptance -[precedes]-> Merge
- (CI Pass -[causes]-> Merge) -[does not hold]
- Merge -[requires]-> Acceptance Mark
- (Merge -[requires]-> Review Approval) -[does not hold]
- Merge -[precedes]-> Reconcile
- Acceptance -[requires]-> Hold Label

### Judgment Routing

- Harness -[consults]-> Adjudicator
- Harness -[performs]-> Rule-defined Operation
- (Harness -[asks {to: Human Maintainer}]-> Rule-defined Operation) -[does not hold]
- (Harness -[performs]-> Irreversible Operation) -[holds when]-> (Human Maintainer -[approves]-> Irreversible Operation)
- (Harness -[resumes]-> Bead) -[holds when]-> (Loom Gate -[becomes]-> Resolved)
- Harness -[measures]-> Wait Label
- Harness -[resolves {by means of: Wait Label}]-> Loom Gate
- ((Harness -[resolves]-> Loom Gate) -[holds when]-> ((Harness -[measures]-> Wait Label) -[does not hold])) -[does not hold]
- Harness -[measures]-> Done Label
- Harness -[measures]-> Hold Label
- (Harness -[files]-> Remedy Bead) -[holds when]-> (Hold Label -[becomes]-> Broken)
- (Remedy Bead -[blocks]-> Hold Guard) -[holds when]-> (Hold Label -[becomes]-> Broken)
- ((Harness -[judges reach of]-> Done Label) -[holds when]-> (Hold Label -[becomes]-> Broken)) -[does not hold]

### Plan History Analysis

- Harness -[reads]-> Audit Log
- Harness -[produces]-> State Interval
- Graph Planner -[uses]-> State Interval

### Untrusted Input

- Bead Text -[is an instance of]-> Untrusted Input
- Change Reason -[is an instance of]-> Untrusted Input
- PR Body -[is an instance of]-> Untrusted Input
- Agent Output -[is an instance of]-> Untrusted Input
- (Harness -[executes]-> Bead Text) -[does not hold]
- (Harness -[executes]-> Change Reason) -[does not hold]
- (Harness -[executes]-> PR Body) -[does not hold]
- (Harness -[executes]-> Agent Output) -[does not hold]
