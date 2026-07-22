# ゴールデンクエリのカナリア運用ガイド

検証済み(verified)のゴールデンクエリも、ウェアハウスのスキーマ変更やデータの
変質で静かに壊れる。このガイドは、検証済みクエリを**評価カナリア**として定期的に
再実行し、壊れたら書き戻す運用手順を示す(OpenAI 社内データエージェントの
ゴールデンクエリ常時評価、text-to-SQL 評価の execution accuracy が下敷き)。

**ochakai は SQL を実行しない**。カナリアの実行
主体はあなたのクライアント — CI ジョブ、または定期実行のエージェント — であり、
ウェアハウスへの資格情報もそちらが持つ。ochakai が提供するのは素材だけである。

## 運用サイクル

### 1. 列挙 — 再検証が古い順に取り出す

```sh
ochakai search --sort verified_at --type 'Golden Query' --status verified --limit 100 --json \
  | jq -r '.hits[] | [.id, .verified_at, .attrs.sql] | @tsv'
```

REST 直接なら
`GET /api/v1/knowledge?type=query&status=verified&sort=verified_at&limit=100`、
MCP なら `search_knowledge` の `sort: "verified_at"` が同じフィードを返す。
`sort=verified_at` は検証日時の古い順(未検証は最後)に返す。「90 日以上
再検証されていない verified query」がカナリアの起点になる。OKF エクスポート
(`ochakai export` / `GET /api/v1/export`)を CI にチェックアウトする方式でもよい。

### 2. 実行 — クライアントの資格情報で

各エントリの `attrs.sql` をウェアハウスで実行する(BigQuery なら `bq query`)。
ochakai はこの工程に関与しない。

### 3. 判定

| 結果 | 判定 |
|---|---|
| 実行エラー(テーブル・カラム消失など) | **失敗** — スキーマ変更でクエリが壊れた |
| 行数・主要集計値が前回から閾値超の変動 | **警告** — データ変質か定義ドリフト |
| `Metric` 型に対応するクエリなら、`POST /api/v1/compile` の出力とも突き合わせ | 乖離があれば **警告** — セマンティック定義とゴールデンクエリがずれている |
| 正常 | 再検証として記録(→ 4) |

### 4. 書き戻し — 認可ではなく記録で

- **正常だった**: `update_knowledge` で `status: verified` のまま更新すると、
  実行者が `verified_by`・現在時刻が `verified_at` に再スタンプされ、
  次回の `sort=verified_at` で後ろに回る。
- **失敗・警告**: 対象ナレッジへ `Insight`(`kind: caveat`)を draft で作成するか、
  `status: deprecated` + `status_note`(理由)への変更を提案する。判断は人間が
  provenance を見て行う。誤りと確定した場合は `status: rejected` + `status_note`。
- **どちらの場合も成果を記録する**: `ochakai report queries/<id> worked` /
  `ochakai report queries/<id> failed --note "何が起きたか"`(REST:
  `POST /api/v1/usage/queries/<id>`、MCP: `report_outcome`)。worked / failed の
  累計が `/usage` に出るので、failed が積んだ verified エントリは
  再検証の優先対象として拾える。**`sort=failed` の再検証フィード**
  (`ochakai search --sort failed --status verified`、REST:
  `GET /api/v1/knowledge?sort=failed`、MCP: `search_knowledge` の
  `sort: "failed"`)が、誤りと報告された順(失敗数の多い順)に列挙する。
  時間ベースの `sort=verified_at` を補う証拠ベースの入口である
  (設計ドキュメント 0025)。

## CI スニペット(GitHub Actions + BigQuery)

```yaml
name: golden-query-canary
on:
  schedule:
    - cron: "0 21 * * 1" # 毎週月曜 06:00 JST
jobs:
  canary:
    runs-on: ubuntu-latest
    permissions:
      id-token: write # Workload Identity 連携(キーレス)
    steps:
      - uses: google-github-actions/auth@v3
        with:
          workload_identity_provider: ${{ vars.WIF_PROVIDER }}
          service_account: ${{ vars.CANARY_SA }}
      - name: run canaries
        run: |
          TOKEN=$(gcloud auth print-identity-token --audiences="$OCHAKAI_URL")
          curl -s -H "Authorization: Bearer $TOKEN" \
            "$OCHAKAI_URL/api/v1/knowledge?type=query&status=verified&sort=verified_at&limit=50" \
          | jq -c '.hits[]' | while read -r hit; do
              id=$(jq -r .id <<<"$hit")
              sql=$(jq -r .attrs.sql <<<"$hit")
              if ! bq query --nouse_legacy_sql --dry_run "$sql" >/dev/null 2>&1; then
                echo "::error::golden query $id no longer compiles against the warehouse"
              fi
            done
```

`--dry_run` はスキーマ変更による破損(工程 3 の「失敗」)をコストゼロで検知する。
結果変動まで見る場合は実実行して前回結果と比較する(行数と主要集計値を
アーティファクトに保存して差分を取るのが簡便)。

## 補助シグナル: 利用テレメトリ

`ochakai usage queries/<id>`(REST: `GET /api/v1/usage/queries/<id>`、MCP:
`get_knowledge_usage`)は、そのクエリが実際に検索ヒット・
取得された回数、worked / failed の報告数、最終利用日時を返す。
**長期間使われていない verified** はカナリアの優先度を下げる(または
deprecated 候補にする)材料になり、**failed が積んだ verified** は逆に
優先的に再検証する材料になる。
