# ochakai 設計ドキュメント 0063: 使われていない2つの推奨型を外す

Status: Accepted(2026-07-31)。[0038](0038-type-vocabulary-realignment.md)
§3.2 の `Playbook` / `API Endpoint` に関する記述と §4 の表を改訂する —
それ以外(`Semantic Model` / `Golden Query` の撤去、`Skill` / `Policy` の
追加、§4.1〜§4.4)は有効。**型の語彙領域の現行ドキュメント**。実装は同 PR。
Date: 2026-07-31

## 1. 目的

推奨型の語彙を 11 型から **9 型**に減らす。外すのは `Playbook` と
`API Endpoint`。この 2 つは 0038(2026-07-27、v0.16.0 でリリース済み)で
足されたばかりだが、issue #353 が指摘したとおり
[0015 §3.1](0015-surface-consistency.md) の下で MCP スキーマは
エージェントのコンテキスト予算であり、その予算を使い続けるだけの理由が
実測できていなかった。

## 2. 0038 の基準は「OKF が綴りを与えたか」であって「誰かがその綴りを
書いたか」ではなかった

0038 §2 は SPEC の例示だけでは根拠にならないとし、判断材料を 2 つ
挙げた: SPEC が producer に課す唯一の要請(descriptive で
self-explanatory であること)と、**OKF が実際に綴りを与えた証拠**
(SPEC の例示、SPEC 本文の worked example、[リファレンスバンドル](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf/bundles)
の実使用)。この基準で 0038 §3.2 は次のように採用した:

| 型 | 0038 が挙げた根拠 |
|---|---|
| `Playbook` | SPEC §4.1 の例示、§4.4 の worked example 全文、§9 のログ例 |
| `API Endpoint` | SPEC §4.1 の例示のみ |

いずれも「OKF という仕様が綴りに触れたか」を問うもので、「ochakai の
利用者が実際にその綴りで書いたか」は問わなかった。SPEC §4.1 自身が
"Type values are not registered centrally" と続ける通り、例示は producer
への手本であって使用実績ではない。予算を払い続けるかどうかを決める
材料としては、後者のほうが近い。

## 3. 実測: どこにも書かれていない

ochakai 自身の `examples/`(`demo`、`bigquery-catalog`)と OKF 公式の
リファレンスバンドル(4 バンドル、78 ファイル)を合わせて数えると:

| 型 | ochakai `examples/` | OKF 公式バンドル |
|---|---|---|
| `BigQuery Table` | ✓ | ✓ ×22 |
| `Reference` | ✓ | ✓ ×20 |
| `Metric` | ✓ | ✓ ×3 |
| `BigQuery Dataset` | – | ✓ ×3 |
| `Policy` | ✓ | ✓ ×2 |
| `Attested Computation` | ✓ | ✓ ×2 |
| `Skill` | ✓ | ✓ ×1 |
| `Insight` | ✓ | – |
| `Glossary Term` | ✓ | – |
| `Playbook` | – | – |
| `API Endpoint` | – | – |

9 型はいずれかに実例がある。`Playbook` と `API Endpoint` だけが両方
ゼロである — SPEC の散文が例に挙げているだけで、OKF 自身も ochakai も、
一度もこの綴りで書いていない。

(余談: OKF 公式バンドルには現行語彙に無い `Log` が 1 回だけ出てくるが、
追加はこの issue の範囲外である — 問いは減らす方だけを検討した。)

## 4. `Playbook` の worked example は、なぜ今回は足りないと判断したか

0038 §3.2 は `Playbook` について、SPEC §4.4 が全文を worked example
として書き下していることを強い根拠とした。だが SPEC の散文例は教材
であって実例ではない — §4.1 が明言する "not registered centrally" の
とおり、例に挙げることと実際に使われることは別である。OKF 公式の
リファレンスバンドル(実運用を模したデータ)にすら一度も現れないことは、
この綴りが実際の需要から生まれたのではなく、説明のために書かれた
という見立てのほうによく合う。`API Endpoint` は SPEC §4.1 の例示
しか根拠が無く、判断はさらに軽い。

## 5. 決定: 推奨語彙は 9 型

| 型 | 何を持つか |
|---|---|
| `Metric` | メトリクス定義、同義語 |
| `Attested Computation` | 認可された計算と、その実行を検証する手段 |
| `Skill` | 手順そのもの — `executor.resource` が指す実行手順 |
| `Insight` | メトリクスの読み方: ベースライン、季節性、注意点、閾値 |
| `Policy` | 数字の決め方を定める規範。`sources[].resource` の典拠になる |
| `Glossary Term` | 用語 |
| `BigQuery Dataset` | データセットのカタログエントリ |
| `BigQuery Table` | テーブルのカタログエントリ |
| `Reference` | 外部資料の写し |

表示順は 0038 のまま(定義 → 実行 → 解釈と規範 → カタログ資産 →
外部の写し)、2 行が抜けるだけである。

型集合は閉じない(0023 のまま)。`Playbook` や `API Endpoint` を書く
ことは変わらずでき、自由型として第一級であり続ける。将来
`examples/bigquery-catalog` などがどちらかを実際に使うようになれば、
そのとき教える語彙に戻せばよい — 外すのは推奨からだけである。

### 5.1 `Insight` のリスクは問わない

issue #353 は「スキーマから型が消えるとエージェントがその型を書かなく
なるかもしれない」という懸念を測定つきで検討するよう求めていた。
`Insight` は README 冒頭の主張が拠って立つ型だが、本ドキュメントは
`Insight` を外さない。問い直したのは実例が一つも無い 2 型だけであり、
残す 9 型はいずれも §3 の表で実例が確認できているので、この懸念は
今回の決定には当たらない。

## 6. 変わらないこと

- 保存済みエントリは触らない。マイグレーションはない(0038 §4.3 の
  まま)。
- サーバーの振る舞いは 1 つも増えない(0038 §4.2 のまま)。
- 語彙を述べる場所は 1 つのまま — `domain.Types` が唯一のソースで、
  静的資産(`api/openapi.yaml`・Web UI・MCP の jsonschema タグ)は
  0038 §4.4 が用意した外側のテストで固定されている。

## 7. 壊れるもの

**保存済みデータと wire 形式は変わらない。** 型は自由文字列のままなので、
`type: Playbook` を送る既存クライアントは今までどおり通る。

- `domain.TypePlaybooks` と `domain.TypeEndpoints` の Go 定数が消える
  (Go API を直接使う組み込みホスト向けの破壊的変更、0038 §6 と同じ
  性格)。
- 補完・ヒント・MCP スキーマ説明・`docs/knowledge.md` の表から 2 つの
  綴りが消える。`--type Playbook` はエラーにならず、自由型として
  照合されるだけである(0023 のまま)。
- MCP ツール説明・スキーマから 2 語減る分、エージェントに渡る
  コンテキストが短くなる — [0015 §3.1](0015-surface-consistency.md)
  が言う予算を、実例のない 2 語について払うのをやめる。
