# トラブルシューティング

ローカルとクライアント側の症状。Google Cloud 側の症状 — `/healthz` を
GFE が横取りする、`NOT_AUTHORIZED`、IAM の伝播遅延 — は
[デプロイガイドの §7](../../deploy/cloudrun/README.md)にある。MCP
クライアントがまったく繋がらない場合は
[MCP クライアントを繋ぐ](mcp-clients.md)にある。

まず `ochakai whoami` から始める。クライアント側の状況をまるごと四行で
示す — どのサーバーか、その選択がどこから来たか、誰として、そして
サーバーが応答するか(`$OCHAKAI_PRODUCER` が設定されていれば
`producer:` の行が五行目として加わる):

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

**書き込みが 403 で失敗し、`mode:` が `read-only` を示す。** デプロイが
`OCHAKAI_MODE`(`read-only` または `public`)を設定している。運用者で
あるあなたのものも含め、すべての書き込みが拒否される — それは呼び出し
側ではなくデプロイの性質である。

## 検索

**`search needs a query`、HTTP 400。** search はクエリか、クエリなしで
一覧できる `sort`(`verified_at`、`usage`、`failed`、`stale_after`)か、
あるリソースを引用しているものを列挙する `--source` のいずれかを必要と
する。すべてを一覧するモードは意図的に存在しない。

**concept があるはずのベースで検索が何も返さない。**

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
- *保存されているはずの語が何にもマッチしない。* rejected と
  soft-delete された concept は既定の検索から除外され、`--status` は
  さらに絞り込む。concept が無いと結論する前にフィルタを外す。
- *embeddings は on なのにランキングが変わらない。* vector は concept
  が書かれたときに書かれるので、その設定より前からある concept には
  ベクトルが無い。`ochakai reembed` を実行する。

**スコアが無意味に見える。** それは尺度ではなく順序であり、lexical と
hybrid のモード間で比較できない。レスポンスを絞るには、スコアの下限で
はなく `budget` を使う。

## concept の書き込み

**`put --only-if-new` で 412。** その id はすでに live な concept が
持っている — `rejected` のものも含む。`--only-if-new` を外すか、別の
id を選ぶ。*soft-delete* された concept への書き込みは、それ以前の
履歴を保ったまま蘇らせる。MCP 経由では、削除前に verified・rejected・
deprecated だったものを蘇らせることは拒否される — 記録済みの判断が
あった場所に真新しい draft を置くことになるからである。REST と CLI
はその蘇りを許す: どちらも人間がキュレーションする面である。

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
素の bundle ファイルとしてインポートされる。予約済みの `index.md`・
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
markdown の concept しか保存できない。これはリクエスト単位ではなく
デプロイ全体の設定である。

**ファイルが拒否される。** 理由はサイズだけである: 1 ファイルあたり
5 MiB が上限で、件数に上限は無い。メディアタイプを理由に拒否される
ことは無い — ファイルが消えて戻ってくる bundle は、渡したはずの
bundle ではない。ファイルが *何であるか* はバイト列を sniff して決ま
り、クライアントの申告では決まらない。ブラウザがそれで何をしてよいか
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

**embedding の設定変更後、semantic search が静かになった。**
`OCHAKAI_EMBEDDINGS` で別のモデルを名指すと、古い vector は残るが新しい
モデルのクエリからは見えない — 検索はモデルで絞るからである。幅が違う
モデルなら vector テーブルはその幅で再構築され、空で戻ってくる。どちらも
ログに残り、curate されたものは何も失われない: vector は元の concept から
導出されるものだからである(design doc
[0073](../design/0073-search-and-when-embeddings-apply.md) §2)。
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
