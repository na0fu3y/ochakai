# golden query をカナリアとして実行する

検証済みの golden query は静かに壊れる: warehouse がカラム名を変え、
テーブルの分割のされ方が変わり、裏側のデータの形が変わる。ナレッジ
ベースはそれに気づかない。このガイドは、検証済みクエリをスケジュール
実行して**評価カナリア**とし、結果を書き戻すやり方を説明する — OpenAI
の社内データエージェントが自社の golden query に対して継続的に行って
いるやり方であり、text-to-SQL 評価の execution-accuracy 指標が使う
手法でもある。

**golden query は `Attested Computation` concept として保存される。**
ochakai はそのための専用の型を持たない(設計ドキュメント
[0038](../design/0038-type-vocabulary-realignment.md)): OKF SPEC §10 が
既にこの概念を定義しているので、golden query は SQL がどの warehouse
で動くかを `runtime` が言い、SQL 本体を `# Computation` の本文フェンス
が持ち、それが答える質問を producer key の `question` が運ぶ、
Attested Computation である。これより前のベースなら、`Golden Query`
型の concept は free type として引き続き動く — 下の `--type` フィルタ
ではその綴りに置き換える。

**ochakai は SQL を実行しない。** カナリアはあなたの側 — CI ジョブや
スケジュール実行されるエージェント — で走り、warehouse の認証情報も
そちら側に留まる。ochakai が渡すのは材料であり、見つかったことを
記録するだけである。

## サイクル

### 1. List — 最後に検証されてから最も間が空いたもの順

```sh
ochakai list verified_at --type 'Attested Computation' --trust human-reviewed --limit 100 --json \
  | jq -r '.hits[] | [.id, .verified_at, .title] | @tsv'
```

hit は concept を手渡すのではなく名指すだけである: 順位付けこそが
フィードの仕事であり、「このうちどれが期限切れか」に答えるために
百件のドキュメントを丸ごと渡せば、実行しない分まで呼び出し元の
コンテキストを消費してしまう。これから実行する分だけ `ochakai get
<id>` で取得する — question、`runtime`、`# Computation` フェンスまで
含めてドキュメントを出力する。

素の REST なら
`GET /api/v1/search?type=Attested%20Computation&trust=human-reviewed&sort=verified_at&limit=100`。
MCP 経由では `search_concepts` に `sort: "verified_at"` を渡せば同じ
フィードが返る。`sort=verified_at` は検証時刻の古い順に並び、未検証の
concept は最後に来る。「90 日間再検証されていない検証済みクエリ」が
カナリア実行の出発点である。CI で OKF export をチェックアウトする
(`ochakai export`、または `Accept: application/gzip` を付けた
`GET /api/v1/bundle/`)のでも同じように動く。

### 2. Run — 自分の認証情報で

各 concept の `# Computation` フェンスを warehouse に対して実行する
(BigQuery なら `bq query`)。どの warehouse かは `runtime` を読んで
判断する。フェンスこそが computation である — SPEC §10.2 が
frontmatter のキーではなくそこに置いているのは、attester が実行を
正規化する対象を一つのコピーだけにするためである。この段階に
ochakai は関与しない。

### 3. Judge

| 結果 | 判定 |
|---|---|
| 実行エラー(テーブルやカラムが無くなっている) | **失敗** — スキーマ変更でクエリが壊れた |
| 行数やヘッドライン集計が閾値を超えて動いた | **警告** — データが変わったか、定義がずれた |
| 正常 | 再検証として記録する(→ 4) |

### 4. Write back — 承認としてではなく記録として

- **クリーンに走った場合**: 再検証を記録する — `ochakai verify
  queries/<id>`(REST: `POST /api/v1/review/queries/<id>` に
  `{"ruling":"verified"}`)。呼び出し元と現在時刻が concept の検証
  台帳に追記され、concept は次の `sort=verified_at` フィードの末尾に
  移る — 台帳が言うのは最後にいつ確認したかだけでなく、何回確認
  されたかでもある。`update` にはこれが言えない: 何も変えない
  update は何も書かないので、「もう一度確かめた、まだ正しい」は
  どこにも残らない。それこそが verify の存在理由である(設計
  ドキュメント 0025 §6)。MCP に対応するツールは無い — 検証は人の
  判断であり、CI で走るカナリアは CI 自身の身元で記録される。
- **失敗または警告の場合**: 影響を受けた concept に対して draft の
  `Insight`(`kind: caveat`)を作るか、理由を書いた `status_note` 付き
  で `status: deprecated` を提案する。人が provenance を見て判断
  する。concept が誤りだと確定したら `ochakai reject <id> --note
  "…"` になる。裁定は人の面(Web UI / CLI)から行う: エージェントが
  カナリアを走らせている場合、検証済み concept の上書きと status の
  変更はどちらも MCP 経由では拒否されるので(設計ドキュメント 0015
  §3.1)、エージェントの出口は下の `report_outcome failed` と、別
  id での draft 作成になる。
- **どちらの場合も、結果を記録する**: `ochakai report queries/<id>
  worked` / `ochakai report queries/<id> failed --note "何が起きた
  か"`(REST: `POST /api/v1/usage/queries/<id>`、MCP:
  `report_outcome`)。worked と failed の合計は `/usage` に出るので、
  失敗が積み上がった検証済み concept を優先して再検証できる。
  **`sort=failed` の再検証フィード**(`ochakai list failed --trust
  human-reviewed`、REST: `GET /api/v1/search?sort=failed`、MCP:
  `search_concepts` に `sort: "failed"`)は、間違いだと報告された
  回数が多い順に並べる。時間にもとづく `sort=verified_at` を補う、
  証拠にもとづく入口である(設計ドキュメント 0025)。

## CI のスニペット(GitHub Actions + BigQuery)

```yaml
name: golden-query-canary
on:
  schedule:
    - cron: "0 21 * * 1" # 月曜 06:00 JST
jobs:
  canary:
    runs-on: ubuntu-latest
    permissions:
      id-token: write # Workload Identity Federation(鍵レス)
    steps:
      - uses: google-github-actions/auth@v3
        with:
          workload_identity_provider: ${{ vars.WIF_PROVIDER }}
          service_account: ${{ vars.CANARY_SA }}
      - name: run canaries
        run: |
          TOKEN=$(gcloud auth print-identity-token --audiences="$OCHAKAI_URL")
          curl -s -H "Authorization: Bearer $TOKEN" \
            "$OCHAKAI_URL/api/v1/search?type=Attested%20Computation&trust=human-reviewed&sort=verified_at&limit=50" \
          | jq -r '.hits[].id' | while read -r id; do
              doc=$(curl -s -H "Authorization: Bearer $TOKEN" \
                -H 'Accept: text/markdown' "$OCHAKAI_URL/api/v1/bundle/$id.md")
              sql=$(printf '%s' "$doc" | sed -n '/^```sql$/,/^```$/p' | sed '1d;$d')
              if ! bq query --nouse_legacy_sql --dry_run "$sql" >/dev/null 2>&1; then
                echo "::error::computation $id no longer compiles against the warehouse"
                curl -s -X POST -H "Authorization: Bearer $TOKEN" \
                  -H 'Content-Type: application/json' \
                  -d '{"outcome":"failed","note":"canary: dry-run failed"}' \
                  "$OCHAKAI_URL/api/v1/usage/$id" >/dev/null
              else
                # 再検証を記録し、台帳に追記する
                # (concept を両方のフィードから外す)
                curl -s -X POST -H "Authorization: Bearer $TOKEN" \
                  -H 'Content-Type: application/json' \
                  -d '{"ruling":"verified"}' \
                  "$OCHAKAI_URL/api/v1/review/$id" >/dev/null
              fi
            done
```

クリーンに走ったときに `verify` を呼ぶべきかどうかは、カナリアが
実際に何を確かめたかで決まる。上のような `--dry_run` が確立するのは
クエリがまだコンパイルできるということだけである。`verified_at` を
進めるのが正直なのは、実際にクエリを実行して結果を比較したときで
ある。dry-run だけで済ませるなら、verify は落として失敗だけを書き
戻す。

`--dry_run` はスキーマ変更による破損 — ステップ 3 の「失敗」の行 —
を無償で捕まえる。結果のドリフトまで見るには、クエリを実行して前回
の実行と比較する。行数といくつかのヘッドライン集計を artifact として
保存し、それを diff するのが簡単なやり方である。

## 補助的なシグナル: usage テレメトリ

`ochakai usage queries/<id>`(REST: `GET /api/v1/usage/queries/<id>`。
MCP には無い — 設計ドキュメント
[0076](../design/0076-two-tools-leave-mcp.md))は、そのクエリが実際に
検索で返された回数や fetch された回数、worked と failed の報告数、
最後に使われたのがいつかを返す。**長いあいだ誰も使っていない検証済み concept** はカナリアの
優先度を下げる理由になる — あるいは deprecated を検討する理由にも
なる。**failure が積み上がった検証済み concept** はその逆で、先に
再検証する理由になる。
