# Product Requirements Document (PRD): プランナー

## この文書の範囲

本文書は、計画をグラフとして組み立てる製品（本文書では「本プランナー」と呼ぶ）が**保証すべき性質**（何を・誰に・なぜ）を書く。それを実現する機構（設計・手順・実装の詳細）は書かない。本プランナーは Loom に含まれない別の製品であり、本文書は Loom の機能や使い方を定めない。Loom を使うために本文書を読む必要は無く、本文書は、Loom の計画を組み立てる側に何を期待できるかを示す。本文書を単体で参照するときは、参考 PRD として扱う。計画を保存する側（Loom）と計画を実行する側（[ハーネス](prd-harness.md)）が保証する性質はそれぞれの文書が持ち、本文書は計画を組み立てる側の性質だけを書く。

参照書式: 節は `N.M`、章全体は `N章` で参照する。

## 1. プロダクト概要 (Product Overview)

- **プロダクト名**: 本文書では「本プランナー」と呼ぶ
- **コンセプト**: エージェントの計画をグラフとして組み立て、Loom に書き込み、実行の結果を読んで計画を組み直す、Agentic グラフエンジニアリング（エージェントが作業と依存をグラフとして設計し、実行の結果を読んでそのグラフを組み直し続けるやり方。1.1）の製品。計画に対する人向けの窓口として利用者に寄り添う。
- **目的**: 利用者が問う前に、計画の穴（間に合わない見通し・収束しない流入・使われない資源・前後の食い違い・成果を満たさない完了）を見つけ、判定を経て計画へ反映し、同じ穴を二度と開けない状態を保つ。

### 1.1 用語 (Terminology)

本文書に独自の語は本節で定義し、Back Matter の英語の Entity 名との対応は Back Matter の各行の説明に「本文の語」として示す。

- **Agentic グラフエンジニアリング**: エージェントが作業と依存を計画のグラフとして設計し、実行の結果を読んでそのグラフを組み直し続けるやり方。本プランナーはこのやり方で計画を受け持つ。
- **計画**: Loom に保存された Bead とリンクのグラフ。本プランナーは計画を読み書きするが、実行はしない（実行はハーネスだけが行う）。
- **ハーネス**: Loom の計画を並列に実行し、結果（状態・理由・費用・PR・実行の記録）を Loom に書き戻す製品（[ハーネスの PRD](prd-harness.md)）。
- **マイルストーン**: 複数の作業を束ねた到達点。コードで測れる完了条件を持つ。
- **見通し**: 待ち行列が、いつ・どの順で消化されるかの予測。
- **流入**: 実行の途中で新しく起票される作業。
- **資源**: 作業を実行する力の供給元（作業を実行するエージェントの実行環境や利用枠など）。資源ごとに、使える期間の終わり（期限）と、単位時間に捌ける作業の量（処理量）を持つ。期限を過ぎた資源には、作業を割り当てられない。
- **正本**: 同じ事実を複数の場所（文書と設定、2 つの記録など）が書くとき、食い違ったら正とすると宣言した一方の場所。
- **判定者**: 設計の分岐と取り消せない操作の可否を決める役割のエージェント。本プランナーの提案は、5 章冒頭が定める範囲を除いて判定者を経て計画へ反映する。
- **期限と割り込み**: 期限（`due_at`）は Bead に付く日付で、本プランナーか利用者が付ける。割り込みは期限に間に合わない見込みになった鎖の根に付く一時的な引き上げで、ハーネスが付ける（[ハーネスの PRD](prd-harness.md) 5.1）。
- **状態の区間**: Bead が Ready・作業中・塞がれ中・終端のいずれかにいた期間。ハーネスが監査ログから再構成して提供する（ハーネスの PRD 5.7）。
- **計画の自己修復**: 計画の穴を利用者より先に見つけ、原因を分類して、既知の型なら判定者を経て計画へ反映し、未知の型なら起票して次から既知の型として扱うところまでを、本プランナーが自分で閉じる性質。提案と起票は、捕まえる穴と変わる判断の両方を言えるものに限る（4.1）。

## 2. 背景と課題 (Background & Problem)

### 2.1 計画は書いた時点から古くなる

計画は起票した時点の見通しで組まれる。実行が進むと流入・処理量・期限が変わり、書かれた順序と優先度は実態からずれていく。ずれを見つけて組み直す主体がいなければ、計画は利用者が気付くまで古いまま実行される。

### 2.2 計画の穴は利用者の問いで見つかっている

「いつ終わるか」「なぜこの順か」「この資源は使われているか」「このマイルストーンは本当に終わったか」は、利用者が対話で問うたときに初めて調べられている。問われなかった穴は、期限を過ぎてから見つかる。

### 2.3 実行は局所で最適化される

ハーネスは実行時に目の前の Ready を最適に捌く。期間をまたぐ配分（どの資源をいつ使い切るか、どのマイルストーンをどの日付までに届けるか）は、実行時の判断からは見えない。

### 2.4 同じ壊れ方が繰り返される

監査履歴と実行記録には、同じ止まり方・同じ食い違いが繰り返し残る。記録を読んで構造の直しを計画に書き、効果を測る主体がいなければ、直しは個別の対処で終わる。

## 3. ターゲットユーザー (Target Audience)

- **一次ユーザー（利用者）**: Loom で計画を管理し、その実行をエージェントに任せたい個人開発者。本プランナーの見通しと提案を読み、人にしかできない判断（利用者自身の成果物の公開・支出・取り消せない操作）だけを受け持つ。
- **判定者**: 本プランナーの提案を受け取り、計画へ反映するかを決める。
- **ハーネス**: 本プランナーが組み直した計画を実行し、その材料となる実行記録を書く。

## 4. コアバリューと解決策 (Core Value & Solution)

### 4.1 計画の自己修復 (Self-repairing Planning)

本プランナーは、分析・提案・反映のすべてに計画の自己修復を当てはめる。計画の穴は利用者の問いを待たずに検知し、既知の型なら判定者を経て計画へ反映し、未知の型なら実測を添えて起票し、次からは既知の型として自分で見つける。検知から反映までを 1 つの振る舞いとし、提案や報告で止めない。反映は判定者を経るのを原則とし、経ずに反映してよい変更の範囲は 5 章冒頭が定める。提案と起票は、捕まえる穴と、それで変わる判断の両方を言えるときだけ出す。本プランナー自身の起票も流入になるので、価値の段は 5.2 の流入の抑制を本プランナー自身にも当てる。価値があるかは文意の判断で機械検査にできないため、判定者が見る。

### 4.2 期間をまたぐ計画を受け持つ (Planning across Time)

本プランナーは、期間をまたぐ配分と順序（見通し・資源の使う順・マイルストーンの日付との前後関係）を受け持つ。資源配分を考慮した実行時の細かい優先度の組み替えはハーネスの責務であり（[ハーネスの PRD](prd-harness.md) 5.1）、本プランナーは行わない。

### 4.3 計画を書き、実行はしない (Plans, Never Executes)

本プランナーは Loom の計画を読み書きするが、作業を claim せず、実行しない。計画の依存をグラフとして検証するような決定論的な処理は、推論の有効性が認められた時点で Loom やハーネスの決定論的な実装へ置き換える。

### 4.4 監査可能性 (Auditable Proposals)

本プランナーの検知・提案・判定・反映は、Loom の変更理由に残る。「なぜこの順にしたか」「なぜ引き上げたか」「なぜ開き直したか」を後から人とエージェントが追える。

## 5. 主要機能要件 (Key Functional Requirements)

5 章のどの機能も、利用者の問いを待たずに行い、計画へ反映するまでを 1 つの振る舞いとする。どの機能にも 4.1 の計画の自己修復を当てはめる。

反映は判定者を経る。判定者を経ずに反映してよい変更は、次の 2 つだけである。(1) 判定者が既に出した結論を、計画へそのまま写す変更（5.7）。(2) 正本が宣言済みで、どちらを正とするかが決まっている食い違いを、正の側に合わせる変更（5.6）。成果を満たさない close の開き直し（5.5）、優先度・順序・範囲の変更（5.1・5.3・5.4）、流入を抑える手（5.2）は、判定者を経て反映する。基準: 判定者を経ずに反映する範囲が広いと、本プランナーの誤りがそのまま計画に入る。狭すぎると、判定の待ちで自律が止まる。

### 5.1 見通し (Forecast)

- 待ち行列を優先度の塊に分け、処理量と流入を差し引いた消化日時を、塊ごとに曜日つきで示す。
- 処理量が変わる日（資源の期限・切り替え）を見通しに織り込む。
- 見通しが公開・出荷の日付や資源の期限に間に合わないと分かったら、間に合わせる手（順序・優先度・範囲の変更）を提案する。
- 見通しの入力に、ハーネスが提供する状態の区間（Ready で待った時間・塞がれ中の時間）を使う。基準: 現在の断面だけでは、需要のある作業がどれだけ待っているかが見えない。
- 見通しは、処理量・流入・資源の期限・マイルストーンの完了のいずれかが変わったときに更新する。基準: 更新の契機が決まっていないと、利用者が読む見通しが古いまま残る。

### 5.2 流入の分析と抑制 (Inflow Control)

- 流入を、起票のきっかけと最上位の親に分けて集計する。集計の軸の一つに、ハーネスが提供する状態の区間（待ち時間・終わり方）を使う。
- close あたりの起票数が待ち行列の収束を妨げていれば、流入を抑える手を提案する。

### 5.3 資源の全体最適 (Resource Allocation)

- 資源ごとの期限から、資源を使う順を決める。
- 消費の経路が無い資源を見つけ、その経路を作る作業を、期限に間に合う段へ上げる。
- 本プランナーが受け持つのは期間をまたぐ配分の計画である。実行時の細かい組み替えはハーネスが行う（4.2）。

### 5.4 優先度の調停 (Priority Mediation)

- 公開・出荷の日付と、公開物を変える作業の見込み日の前後関係を照合し、日付の前に要る作業を引き上げる。
- 日付に間に合わせる分担は次のとおり。期限（`due_at`）は本プランナーか利用者が付け、本プランナーは割り込みを付けない。今の処理量で期限に間に合わない見込みの判定と、割り込みを付けることはハーネスが行う（ハーネスの PRD 5.1）。基準: 期限を付ける側と割り込みを付ける側が同じだと、見込みの判定が自分の付けた期限を入力にする帰還路になる。
- 同じ対象を触る作業には、衝突を避ける順序を付ける。
- マイルストーンの間の順序と優先度を、到達の日付に沿って調停する。

### 5.5 マイルストーンの完了の検証 (Milestone Verification)

- マイルストーンの完了条件が、題の成果を表しているかを照合する。
- 成果を満たさない close を開き直す。
- 完了に要る作業がマイルストーンの外にあれば、中へ入れる。
- マイルストーンを方針として廃止するときは、Loom の廃止の印を付ける（印の付け方と Loom 側の挙動は [milestone.md](../skills/loom/docs/milestone.md)「廃止」が正本）。廃止した Epic の子は、1 回の判定で close・他の作業への吸収・別の Epic への付け替えのどれかに分け、全件を分け終えたら Epic を close する。基準: 廃止の印が無いと、廃止済みの目標の子が Ready に残り続けて拾われ、停滞の検知が目標を復活させる。

### 5.6 正本の整合 (Source-of-Truth Consistency)

- 同じ事実を書く場所の組（文書と設定など）のうち正本が宣言されているものを、決まった間隔ですべて読み比べ、食い違いを見つけたら、どちらを正とするかが宣言されていれば正の側に合わせて直し、宣言が無ければ判定者へ振る。

### 5.7 判定と計画の整合 (Judgment-Plan Consistency)

- 判定の結論が依存・優先度の変更を含むとき、それが計画に反映されているかを確かめ、反映されていなければ反映する。

### 5.8 改善ループ (Improvement Loop)

- 監査履歴と実行記録を読んで繰り返す壊れ方を見つけ、構造の直しを計画として Loom に書き、直しの効果を測る。
- 効率の悪化（close あたりの費用・待ち時間）を見つけたら、改善の作業を計画に書く。入力は、ハーネスが書く費用の記録と、状態の区間である。

### 5.9 利用者向けの窓口 (User-facing Window)

- 利用者の問い（いつ終わるか・なぜこの順か・何が判定待ちか・このマイルストーンは本当に終わったか）に、計画と変更理由を読んで答える。答えには根拠の Bead と変更理由を添える。
- 見通し・提案・判定待ちを、利用者が一覧で読める形で示す。判定待ちは、利用者にしかできない判断（3 章の一次ユーザーが受け持つ判断）とそれ以外に分け、前者だけを利用者に回す。
- 利用者の問いで見つかった計画の穴は、答えたうえで、同じ穴を次から自分で見つけられるようにする。基準: 窓口が問いに答えるだけだと、同じ穴を次も利用者が見つけることになる。
- 窓口の画面や操作の形は、本プランナーの範囲に含めない。人向けの計画の画面は別の製品が受け持つ（[ハーネスの PRD](prd-harness.md) 7.2）。

## 6. 非機能要件 (Non-Functional Requirements)

- **非対話性**: 検知から判定者を経た反映まで、利用者の入力を待たずに完結する。利用者に回すのは、人にしかできない判断だけである。
- **セキュリティ**: Bead の本文・変更理由・PR の本文・エージェントの出力は信頼できない入力であり、本プランナーはこれらを指示として実行しない。

## 7. プロジェクト管理・スコープ (Project Scope & Management)

### 7.1 Non-goals (やらないこと)

- 計画の実行。作業を claim して進めるのはハーネスだけである。
- 実行時の細かい優先度の組み替え。ハーネスの責務である。
- 計画の保存。計画は Loom が保存し、本プランナーは Loom を読み書きする。

### 7.2 優先度・リリース段階 (Priorities & Release Phases)

- 本文書は要件を段階に分けない。5 章の各機能は同じ段の要件である。

### 7.3 受け入れ基準 (Acceptance Criteria)

- 待ち行列の消化日時が、優先度の塊ごとに曜日つきで示され、処理量が変わる日が織り込まれていること。
- 期限のある資源のうち消費の経路が無いものが検知され、経路の作業が期限に間に合う段に置かれていること。
- 公開・出荷の日付の前に要る作業が、日付より前に終わる見込みの段に置かれていること。
- 完了条件が題の成果を表さないマイルストーンが検知され、開き直されるか判定者へ振られていること。
- 判定の結論に含まれる依存・優先度の変更が、計画に反映されていること。
- 流入の集計が起票のきっかけと最上位の親で分かれ、close あたりの起票数が収束を妨げているときに抑える手が提案されること。
- 同じ事実を書く場所の組の食い違いが、正の宣言があれば正の側へ直され、宣言が無ければ判定者へ振られること。
- 繰り返す壊れ方に構造の直しが計画として書かれ、直しの前後で壊れ方の件数が比べられていること。
- 分類に無い型の穴が、実測を添えて 1 件だけ起票され、次から既知の型として検知されること。
- 利用者の問いに、根拠の Bead と変更理由を添えて答えられ、判定待ちが利用者にしかできない判断とそれ以外に分かれて示されること。
- 判定者を経ずに反映した変更が、5 章冒頭の 2 つの範囲に収まっていること。

---

本文の各要件は、下の Back Matter のいずれかの行に当たる（Main section ⊆ Back Matter）。

# Back Matter

- The following sections represent a pure knowledge graph, stripped of narrative.
- 各行は「英語の名前: 説明（本文の語・節）」の形で、英語の名前が本文のどの語に当たるかを括弧に示す。

## Entities

### Classes

- Planner: 計画をグラフとして組み立てる製品の集合（本文の「プランナー」）。
- Plan Gap: 計画の穴（間に合わない見通し・収束しない流入・使われない資源・前後の食い違い・成果を満たさない完了）の集合（本文の「計画の穴」・1 章）。
- Resource: 期限と処理量を持つ実行の枠の集合（本文の「資源」・1.1）。
- Untrusted Input: 本プランナーが指示や仕様の入力にしてはならない入力の集合（本文の「信頼できない入力」・6 章）。

### Entities

- Graph Planner: 本製品（本文の「本プランナー」）。
- Loom: 計画を保存するトラッカー（本文の「Loom」）。
- Harness: 計画を実行する製品（本文の「ハーネス」・1.1）。
- Plan: Loom に保存された Bead とリンクのグラフ（本文の「計画」・1.1）。
- Bead: 計画の作業 1 件（本文の「作業」「Bead」）。
- Milestone: 完了条件を持つ到達点（本文の「マイルストーン」・1.1）。
- Forecast: 待ち行列の消化日時の予測（本文の「見通し」・5.1）。
- Inflow: 実行の途中で起票される作業（本文の「流入」・5.2）。
- Expiring Resource: 期限を持つ資源（使える期間の決まった利用枠など）の 1 つ（本文の「期限のある資源」・5.3）。
- Consumption Path Work: 資源を消費する経路を作る作業（本文の「消費の経路を作る作業」・5.3）。
- Priority Order: 作業とマイルストーンの順序と優先度（本文の「順序と優先度」・5.4）。
- Pre-release Work: 公開・出荷の日付の前に要る作業（本文の「日付の前に要る作業」・5.4）。
- Conflicting Work: 同じ対象を触る作業（本文の「同じ対象を触る作業」・5.4）。
- Unmet Close: 成果を満たさないまま close された作業（本文の「成果を満たさない close」・5.5）。
- Outside Work: 完了に要るがマイルストーンの外にある作業（本文の「マイルストーンの外にある作業」・5.5）。
- Source of Truth: 宣言された正本（本文の「正本」・1.1・5.6）。
- Adjudicator: 判定者の役割のエージェント（本文の「判定者」・1.1）。
- Adjudication Result: 判定の結論（依存・優先度の変更を含む）（本文の「判定の結論」・5.7）。
- Detected Gap: 本プランナーが見つけた計画の穴の 1 件（本文の「検知した穴」・7.1）。
- Gap Type: 計画の穴の型（本文の「穴の型」・4.1）。
- Plan Change: 本プランナーが提案する計画の変更（本文の「提案」・4.1）。
- User-reported Gap: 利用者が対話で先に指摘した計画の穴の 1 件（本文の「利用者が対話で指摘した計画の穴」・7.1）。
- Audit History: Loom の監査履歴（本文の「監査履歴」・5.8）。
- Run Record: ハーネスが書く実行の記録（本文の「実行記録」・5.8）。
- Structural Fix: 繰り返す壊れ方を止める構造の直し（本文の「構造の直し」・5.8）。
- Deterministic Check: 同じ入力に同じ答えが決まる処理（依存のグラフとしての検証など）（本文の「決定論的な処理」・4.3）。
- Runtime Reprioritization: 実行時の細かい優先度の組み替え（本文の同じ語・4.2）。
- Long-range Allocation: 期間をまたぐ配分と順序（本文の同じ語・4.2）。
- Bead Text: Bead の本文（本文の同じ語・6 章）。
- Change Reason: Loom の変更理由（本文の「変更理由」・4.4）。
- PR Body: PR の本文（本文の同じ語・6 章）。
- Agent Output: エージェントの出力（本文の同じ語・6 章）。
- Due Date: Bead の期限（`due_at`）（本文の「期限」・1.1）。
- Expedite: ハーネスが鎖の根に付ける一時的な引き上げ（本文の「割り込み」・1.1）。
- State Interval: Bead が 1 つの状態にいた区間。ハーネスが提供する（本文の「状態の区間」・1.1）。
- Query: 利用者が計画について問う事象の対象（本文の「利用者の問い」・5.9）。
- Wait List: 判定待ちの一覧（本文の「判定待ち」・5.9）。
- Human-only Decision: 利用者自身の成果物の公開・支出・取り消せない操作のように、利用者にしかできない判断（本文の同じ語・5.9）。

### Events

- Detection: 本プランナーが計画の穴を見つける事象（本文の「検知」・4.1）。
- Proposal: 本プランナーが計画の変更を提案する事象（本文の「提案」・4.1）。
- Adjudication: 判定者が提案を決める事象（本文の「判定」・4.4）。
- Reflection: 判定の結論が計画へ反映される事象（本文の「反映」・5 章冒頭）。
- Filing: 未知の型の穴を実測つきで起票する事象（本文の「起票」・4.1）。
- Direct Reflection: 判定者を経ずに計画へ反映する事象（本文の「判定者を経ずに反映」・5 章冒頭）。

### States

- Known Gap Type: 次から本プランナーが自分で見つけられる穴の型の状態（本文の「既知の型」・4.1）。
- Unknown Gap Type: まだ分類に無い穴の型の状態（本文の「未知の型」・4.1）。

## Monosemantic Synapses

### Product Identity

- Graph Planner -[is an instance of]-> Planner
- Graph Planner -[composes]-> Plan
- Loom -[stores]-> Plan
- Harness -[executes]-> Plan
- Plan -[has part]-> Bead
- (Graph Planner -[executes]-> Plan) -[does not hold]
- (Graph Planner -[claims]-> Bead) -[does not hold]
- Graph Planner -[performs]-> Long-range Allocation
- (Graph Planner -[performs]-> Runtime Reprioritization) -[does not hold]
- Harness -[performs]-> Runtime Reprioritization
- Deterministic Check -[has attribute]-> "推論の有効性が認められた時点で Loom かハーネスの決定論的な実装へ置き換える"

### Planning

- Graph Planner -[emits]-> Forecast
- Forecast -[has attribute]-> "優先度の塊ごとの消化日時を曜日つきで示し、処理量が変わる日を織り込む"
- Graph Planner -[analyzes]-> Inflow
- Expiring Resource -[is an instance of]-> Resource
- Graph Planner -[allocates]-> Expiring Resource
- (Graph Planner -[raises]-> Consumption Path Work) -[holds when]-> "期限のある資源に消費の経路が無い"
- Graph Planner -[mediates]-> Priority Order
- Graph Planner -[raises]-> Pre-release Work
- Graph Planner -[orders]-> Conflicting Work
- Graph Planner -[verifies]-> Milestone
- Graph Planner -[reopens]-> Unmet Close
- Graph Planner -[moves {to: Milestone}]-> Outside Work
- Graph Planner -[checks]-> Source of Truth
- Graph Planner -[sets]-> Due Date
- (Graph Planner -[attaches]-> Expedite) -[does not hold]
- Harness -[attaches]-> Expedite
- Harness -[provides]-> State Interval
- Graph Planner -[reads]-> State Interval
- Forecast -[has attribute]-> "処理量・流入・資源の期限・マイルストーンの完了のいずれかが変わったときに更新する"
- Graph Planner -[answers]-> Query
- Graph Planner -[presents]-> Wait List
- Wait List -[has attribute]-> "Human-only Decision とそれ以外に分ける"
- Graph Planner -[escalates]-> Human-only Decision
- Query -[addresses]-> Plan
- Graph Planner -[verifies {with: Adjudication Result}]-> Plan

### Improvement Loop

- Harness -[writes]-> Run Record
- Graph Planner -[reads]-> Run Record
- Graph Planner -[reads]-> Audit History
- Graph Planner -[writes {to: Plan}]-> Structural Fix
- Graph Planner -[measures]-> Structural Fix

### Self-recovery

- Detection -[precedes]-> Proposal
- Proposal -[precedes]-> Adjudication
- Adjudication -[precedes]-> Reflection
- Detected Gap -[is an instance of]-> Plan Gap
- Graph Planner -[proposes]-> Plan Change
- Plan Change -[addresses]-> Detected Gap
- (Graph Planner -[proposes]-> Plan Change) -[holds when]-> "捕まえる穴と、それで変わる判断の両方を言える"
- Adjudicator -[judges value of]-> Plan Change
- Graph Planner -[consults]-> Adjudicator
- (Graph Planner -[performs]-> Direct Reflection) -[holds when]-> "判定者が既に出した結論をそのまま写す、または正本が宣言済みで正の側に合わせる"
- Graph Planner -[applies Inflow Control to]-> Graph Planner
- Detected Gap -[has attribute]-> Gap Type
- (Detection -[causes]-> Filing) -[holds when]-> (Gap Type -[becomes]-> Unknown Gap Type)
- Graph Planner -[classifies {resulting state: Known Gap Type}]-> Gap Type
- User-reported Gap -[is an instance of]-> Plan Gap

### Untrusted Input

- Bead Text -[is an instance of]-> Untrusted Input
- Change Reason -[is an instance of]-> Untrusted Input
- PR Body -[is an instance of]-> Untrusted Input
- Agent Output -[is an instance of]-> Untrusted Input
- (Graph Planner -[executes]-> Bead Text) -[does not hold]
- (Graph Planner -[executes]-> Change Reason) -[does not hold]
- (Graph Planner -[executes]-> PR Body) -[does not hold]
- (Graph Planner -[executes]-> Agent Output) -[does not hold]
