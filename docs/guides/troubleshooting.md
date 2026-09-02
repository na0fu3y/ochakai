# トラブルシューティング

ローカルとクライアント側の症状。Google Cloud 側の症状 — `/healthz` を
GFE が横取りする、`NOT_AUTHORIZED`、IAM の伝播遅延 — は
[デプロイガイドの §7](../../deploy/cloudrun/README.md#7-troubleshooting-in-a-security-hardened-org)にある。MCP
クライアントがまったく繋がらない場合は
[MCP クライアントを繋ぐ](mcp-clients.md)にある。

まず `ochakai whoami` から始める。クライアント側の状況をまるごと四行で
示す — どのサーバーか、その選択がどこから来たか、誰として、そして
サーバーが応答するか(`$OCHAKAI_PRODUCER` が設定されていれば
`producer:` の行が `identity:` の次に加わる):

```
server:    http://localhost:8080 ($OCHAKAI_URL)
identity:  human:anonymous (plain http, no credentials)
health:    ok
mode:      read-write
```

## CLI

**`ochakai: unknown command "--url"`。** フラグはサブコマンドの後に置く、
前ではない: `ochakai whoami --url http://localhost:8080` であり、
`ochakai --url … whoami` ではない。`ochakai` の直後の単語は常に
コマンドである。

**`health: error: … connect: connection refused`。** `server:` 行に
出ているサーバーが listen していない。その行はその選択がどこから来たか
も示す — `$OCHAKAI_URL`、`--url`、あるいは `ochakai use` の選択 —
そして実際のバグはたいていここにある: 以前のセッションから
`OCHAKAI_URL` を export したままのシェルは、毎回 `ochakai use` を
上書きする。

**`server answered 401 Unauthorized: resolve Google ID token: … gcloud
auth print-identity-token: ERROR: … Reauthentication failed`。**
gcloud のセッションが再認証を求めている。CLI は資格情報を作れなければ
無認証のまま送り、identity を読まないデプロイ(公開デモやサンドボックス)
はそれで普通に答える — このメッセージが出るのは、**identity を求めた
デプロイに当たったとき**だけである。`gcloud auth login` を対話シェルで
実行する。エージェントや CI から出ているなら、そこはプロンプトに答え
られないので、サービスアカウントの ADC を使う
([mcp-clients.md](mcp-clients.md#what-the-bridge-needs))。
`ochakai whoami` の identity 行が `human:anonymous (no Google
credentials found)` になっているのが同じ状態の別の見え方である。

**書き込みが 403 で失敗し、`mode:` が `read-only` を示す。** デプロイが
`OCHAKAI_MODE`(`read-only` または `public`)を設定している。運用者で
あるあなたのものも含め、すべての書き込みが拒否される — それは呼び出し
側ではなくデプロイの性質である。

## 検索

**`sort=usage lists concepts; it cannot be combined with a search query`、
HTTP 400。** フィードは順に読むキューであって、関連度で並べ替える対象では
ない。テキストで絞るならクエリだけを、キューを歩くならフィードだけを渡す。
**どちらも渡さなければ列挙になる** — フィルタが一致する concept が id 順に
並び、フィルタも無ければベース全体である(`ochakai list --prefix
teams/growth`)。

**concept があるはずのベースで検索が何も返さない。**

- *答えが「退化している」と言っている。* `ochakai search` が stderr に
  `note:` の行を出す、Web UI が結果の上にそう書く、応答が
  `degraded: true` を持つ — どれも同じ一つのことで、**そのときクエリを
  埋め込めなかった**という意味である。ランキングは lexical だけに落ちて
  いて、語ではなく意味で一致する concept は入っていない
  ([0114](../design/0114-a-degraded-ranking-says-so.md))。問いの言い方の
  問題ではないので、言い換えても直らない — 見るのは[起動](#startup)の
  ログのほうで、そのデプロイが Vertex AI を呼べているかである。埋め込み
  を設定していないデプロイはこれを立てない(そこでは lexical が答えその
  ものであって、落ちた結果ではない)。
- *ひらがなだけの日本語クエリがヒットしない。* lexical 側は日本語を
  二文字の窓に切り、漢字かカタカナを含む窓だけを残す — ひらがなだけの
  窓は文法であり、ほとんど何にでもマッチしてしまうからである。助詞では
  なく名詞で検索する — あるいは embeddings が on になっているか確認する
  ([起動](#startup)の下の起動ログを見る: Google Cloud では既定で on
  だが、それはサービス identity が Vertex AI を呼べる場合に限る)。
- *英語の質問が的外れな concept を返す。* lexical 検索は bag of words
  であり、機能語を三つ共有する concept が、主題そのものを言い当てている
  concept より上位に来ることがある。これは embeddings が直す場合であ
  る。
- *`--trust human-reviewed` を付けると何も返らない。* trust tier は
  このインスタンスが観測した裁定から決まる。取り込んだばかりのベース
  では誰も裁定していないので、この絞り込みは空になる — バンドルの
  `verified:` は文書の主張として `received` に残るだけである
  ([merge は verify ではない](git-review.md#merge-は-verify-ではない))。
  `ochakai verify <id>` か Web UI の「検証」を一手打つ。
- *検証したはずの concept が unverified に戻っている。* tier に数え
  られるのは**いまの内容**を確認した検証だけで、検証のあとの編集・
  移動(rename は本文リンクの書き換えを伴う)で検証は立たなくなる
  ([0138](../design/0138-a-verification-stands-until-the-content-moves.md))。
  台帳は消えていない — `ochakai get` の `verified` に履歴はそのまま
  ある。`ochakai list verified_at --trust unverified` がそういう
  concept を先頭に並べるので、内容を確かめて verify し直す。
- *保存されているはずの語が何にもマッチしない。* soft-delete された
  concept は既定の検索から除外され(却下されたものはそこにも無い —
  却下は削除である)、`--status` はさらに絞り込む。concept が無いと
  結論する前にフィルタを外す。
- *embeddings は on なのにランキングが変わらない。* vector は concept
  が書かれたときに書かれるので、その設定より前からある concept には
  ベクトルが無い。`ochakai reembed` を実行する。

**スコアが無意味に見える。** それは尺度ではなく順序であり、lexical と
hybrid のモード間で比較できない。返る件数を絞るには、スコアの下限では
なく `--limit` を使う([0068](../design/0068-how-a-face-is-added-and-removed.md)
§3 がスコアの床を持たないと決めている)。

## concept の書き込み

**`put --only-if-new` で 412。** その id はすでに live な concept が
持っている。`--only-if-new` を外すか、別の id を選ぶ。*soft-delete*
された concept への書き込みは、それ以前の履歴を保ったまま蘇らせる。
MCP 経由では、削除前に verified・deprecated だったものを蘇らせること
は拒否される — 記録済みの判断があった場所に真新しい draft を置くこと
になるからである。REST と CLI はその蘇りを許す: どちらも人間が
キュレーションする面である。**却下された id はこれに当たらない**:
却下は削除であり、その墓標は普通の墓標なので、MCP からも書き直せる
(設計ドキュメント
[0135](../design/0135-a-rejection-is-a-deletion.md) §3)。

**`put --if-match` で 404。** `--if-match` は既存の concept を置き
換えるものであり、新規作成はしない。

**`put --if-match` で 412。** 送った `If-Match` が concept の現在の
`ETag` と一致していない — 間に誰かが書き込んでいる。concept を読み
直し、変更を当て直して再試行する。何も書き込まれていない。

**`invalid type "…"`。** type は一行、128 バイトまで。収まる値ならどれも
合法である — 推奨語彙はあくまで推奨であり閉じた集合ではなく、`acme/Table`
のような `/` を含む型も OKF SPEC §4.1 のとおり合法である — が、複数行の
文字列は合法ではない。

**エージェントが `cannot replace … it is verified` を受け取る。** 意図
通りの動作: MCP は curate 済みの concept の上書きや削除を拒否する。
[FAQ](../faq.md#can-an-agent-overwrite-or-delete-knowledge-a-human-verified)
を参照。

## インポートとエクスポート

**`import` がファイルのスキップを報告する。** スキップは `skip:` の
プレフィックスを付けて stderr に出力され、サマリで数えられる
(`imported N concepts (…, M skipped)`)。スキップに見えて数えられない
二つのこと: frontmatter に `type` が無いファイルはスキップではない —
type はパスから推測されることは無いので(design doc 0075 §2)、代わりに
素の bundle ファイルとしてインポートされる。ただし `.md` は concept の
住所なので(design doc 0100)、`notes/meeting.md` は
`notes/meeting.markdown` として入り、note がそう言う — バイト列は失わ
れず、綴りだけが動く。予約済みの `index.md`・
`log.md`、あるいは隠しパスは黙って捨てられ、出力にも件数にも現れない。
スキップであるもの:

- ファイルがそもそも保存できない — 空、サイズ上限超過、あるいは住所に
  できないパス;
- サーバーが拒否した場合。このときメッセージはサーバー自身の文句を
  運び、bundle の残りはそのままインポートされる;
- その concept がインポートされなかったファイルである。

**すべてが想定より一段深いディレクトリにインポートされる。** 詰められ
た形がそのまま構造になる: 単一のディレクトリで包まれたアーカイブは、
そのディレクトリの下にインポートされる — bundle が自分の namespace を
保つためである。これは意図的な挙動であり、望まないならアーカイブを
先に展開する。

**Import が `unchanged` と言う。** 保存済みのものと同一の concept は
そのままにされ、書き直されない — 再インポートが revision の履歴を
コピーで埋めないようにするためである。

## ファイル

**ファイルの `put` で 501。** そのインスタンスには `OCHAKAI_GCS_BUCKET` が無く、
markdown の concept しか保存できない。デプロイ全体の設定であり、**打つ前に分かる**
— `ochakai stats` の `files off` の一行と、面を描かない Web UI がそう言う([0131](../design/0131-a-deployment-says-what-it-cannot-do.md))。

**ファイルが拒否される。** 理由は三つしか無い: 空であること、1 ファイル
あたり 5 MiB の上限を超えること、そして ochakai が住所にできない名前
(パスの一区切りでない名前、あるいは `*.md`)であること。件数に上限は
無い。メディアタイプを理由に拒否されることは無い — ファイルが消えて
戻ってくる bundle は、渡したはずの bundle ではない。ファイルが
*何であるか* はバイト列を sniff して決まり、クライアントの申告では
決まらない。ブラウザがそれで何をしてよいか
は配信時に決まる: 画像・PDF・プレーンテキストはその場でレンダリング
され、それ以外はすべて sandbox 下のダウンロードとして渡される — SVG
や HTML ファイルがスクリプトを走らせる origin を持つことは無い。

**ファイルの中身が検索に出てこない。** ファイル名はどの検索でも常に
マッチするが、中身は embeddings が on のときだけ合流し、画像や PDF は
ファイルに対応したモデルを必要とする —
`OCHAKAI_EMBEDDINGS=projects/<p>/locations/global/publishers/google/models/gemini-embedding-2`
(location は `global`・`us`・`eu` のいずれか)。その設定より前から
あるファイルは遡って埋められない — `ochakai reembed`。

## Web UI

**API には concept があるのに UI が空。** `ochakai ui` は *自分が*
選んだサーバーに対して UI を配信する — それは直前に curl した先とは
限らない。起動時のログと `ochakai whoami` のどちらもどのサーバーかを
示す。

**編集が誤った identity で記録される。** `ochakai ui` は loopback では
あなたとして振る舞う。デプロイされた `ochakai serve-ui` は代わりに
サービスアカウントを記録する — `OCHAKAI_IAP_AUDIENCE` が設定され、
webui のサービスアカウントがサーバーの `OCHAKAI_DELEGATING_CALLERS`
に載っている場合を除く。それがブラウザでの編集を
`human:you via process:webui-sa` に変える(design doc 0065 §5)。

<a id="startup"></a>

## 起動

**`docker compose … up` が `Bind for 0.0.0.0:8080 failed: port is
already allocated` で止まる。** 8080 は取り合いになりやすい番号で、
別のインスタンスや無関係なプロセスが先に握っている。
`deploy/compose.yaml` はポートを固定しているので、上書きは
override ファイルで行う(`docker compose -f deploy/compose.yaml -f
ports.yaml up -d`):

```yaml
services:
  ochakai:
    ports: !override
      - "8123:8080"
```

`ochakai use http://localhost:8123` で指し直す。**先に握っていたのが
別の ochakai だった場合は、書き込む前に必ず `ochakai whoami` で
`server:` 行を確かめること** — 起動に失敗したことに気づかないまま
import すると、書き込み先は隣のインスタンスである。

**`/mcp` だけが `Forbidden: invalid Host header "…"` を返す。** 認証は
通っていて、同じプロセスの `/api/v1` は普通に答えるのに、MCP の呼び出し
だけが 403 になる。**前段が loopback で ochakai に繋いでいる構成**が
これを起こす — トンネル(`cloudflared`・`ngrok`)、サイドカーのプロキシ、
手元の TLS 終端。MCP の SDK は「listen している側のアドレスが loopback
で、`Host` がそうでない」ときに DNS rebinding を疑って断る仕様であり、
ブラウザのページが手元のサーバーを狙う形と、トンネルが公開名で前に立つ
形は、プロセスの中から区別が付かない。**Cloud Run では起きない** —
コンテナは loopback でないアドレスで受けるからである。手元で直すには、
前段の向き先を `127.0.0.1` から LAN のアドレス(コンテナなら
そのアドレス)に変える。同じトークンでそのまま通る。

**embedding の設定変更後、semantic search が静かになった。**
`OCHAKAI_EMBEDDINGS` で別のモデルを名指すと、古い vector は残るが新しい
モデルのクエリからは見えない — 検索はモデルで絞るからである。幅が違う
モデルなら vector テーブルはその幅で再構築され、空で戻ってくる。どちらも
ログに残り、curate されたものは何も失われない: vector は元の concept から
導出されるものだからである(design doc
[0080](../design/0080-search-and-how-a-deployment-embeds.md) §3)。
`ochakai reembed` を実行して埋め直す、これがお金を使う工程である。
それが終わるまでランキングは lexical のみになる。**0.18.0 へ上げた
直後にこれが起きたなら**、原因は退役した `OCHAKAI_VERTEX_MODEL` /
`OCHAKAI_EMBEDDING_DIM` が無視されて既定に戻ったことである
([Upgrades](operating.md#upgrades))。

**`pgvector is required for semantic search, and this role may not
create it`。** 管理者として一度 `vector` を作成する — デプロイガイドの
§3 の bootstrap SQL がまさにそれを行う。`OCHAKAI_EMBEDDINGS` でモデルを
名指したデプロイは、それをするまで起動を拒否する — semantic search を
求めたからである。プロジェクトを自動検出しただけのデプロイはこれを
ログに残して lexical のみで動く。

**ログに `semantic search off: Vertex AI did not answer for this
deployment` と出る。** 起動時のプローブが拒否された — ほとんどの場合、
サービス identity に `roles/aiplatform.user` が無いか、
`aiplatform.googleapis.com` が有効化されていない。付与して再起動する
(デプロイガイド §4): embeddings は Google Cloud では既定で on だが、
実際に on にできるのは IAM だけである。

**ログに `files disabled` や `semantic search off…` のいずれかの行が
出る。** これらは、そのインスタンスが持っていないサブシステムを確認
する起動時のログである。あくまで情報であり、稼働中のインスタンスが
実際に何を有効にしているかを確かめる一番速い方法でもある — それぞれ
の意味は[operating.md](operating.md)にまとまっている。
