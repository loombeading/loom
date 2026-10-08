## 停止したエージェントの claim 回収

停止したエージェントが `in_progress` のまま残した Bead は、次の手順で回収する。

1. `lm list --status in_progress` で現在 `in_progress` の Bead を確認する。`--updated-before <日時>`（RFC3339 または `YYYY-MM-DD`）を添えると、一定時刻より前から更新のない Bead だけに絞り込める。例: `lm list --status in_progress --updated-before 2026-09-01`
2. 更新が止まっている（停止したエージェントが残した）Bead を見つけたら、`lm update <id> --release --force --reason "<回収理由>"` で強制解放する（`--force` は他 actor が claim した Bead を解放するためのフラグ）。

補足: claim を持つのが、止まると起動し直されて自分の claim から作業を再開する実行者だけなら、その実行者を起動し直せば claim は再開される。この運用では先に実行者を戻し、それでも再開されない claim だけを上の手順で回収する。
