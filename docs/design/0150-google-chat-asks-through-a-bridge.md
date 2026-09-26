# ochakai 設計ドキュメント 0150: Google Chat は橋を通して問う

Status: Accepted(2026-09-26)。ROADMAP のデータエージェントの第四段
「人がもう問うている場所へ」の最初の一つ。デプロイ自身のエージェントが
REST の一本の後ろにいること([0145](0145-four-faces-and-an-answer-that-shows-its-result.md)
§0・§7)は動かない — Chat は、serve-ui と同じく、その一本の**呼び出し元**
として足される
Date: 2026-09-26

## 0. この記録が決めたこと

1. **Google Chat で問われたら、`ochakai serve-chat` が本人の代わりに
   エージェントに問い、答えを Chat に返す**(§2)。`serve` と同じイメージを、
   serve-ui と同じく別の Cloud Run サービスとして動かす。
2. **secret を一つも増やさない。** Chat の呼び出しは Google が署名した
   ID トークンで届き、橋はそれを Google の公開鍵で確かめる。`serve` には
   自分のサービスアカウントで届き、本人の名前は既存の委譲
   (`Ochakai-On-Behalf-Of`、[0065](0065-identity-and-provenance.md) §3)で
   渡す。答えは HTTP の応答として返し、Chat API は呼ばない(§3)。
3. **SQL は Chat では走らない。** 本人のトークンが無いからである。提案は
   SQL として見せ、Web UI のエージェントへ誘う(§4)。
4. **Slack の橋は作らない。** 署名の secret とボットのトークンが要る(§6)。

## 1. 誰が詰まったか

ochakai を入れている会社の人たちは、Google Chat・Claude・Slack で問う
(2026-09-26、運用者の報告)。Web UI のエージェントは、問いに行く場所を
一つ増やす。**問いは、人がもう問うている場所で受けるほうが届く** — ROADMAP
の第四段がそれで、第一段から第三段が作った答えの質は、そこへ届いて初めて
使われる。

三つのうち最初に Chat を選ぶのは、**姿勢も脅威モデルも動かさずに済む**
からである。Chat は Cloud Run を公開せずに呼べる(Chat のサービス
アカウントにだけ invoker を与える)。Claude のコネクタは公開到達を要し、
[0116](0116-the-connector-price-changed-not-its-condition.md) §3 が数えた
代金を払う — それは次の記録が持つ。

## 2. 形: 別のサービスで、REST の呼び出し元である

橋は、Chat のイベントを受け、`POST /api/v1/agent` に問いを一つ送り、答えを
Chat の形で返すだけである。

- **エージェントに第二の入口を作らない**(0145 §7)。`serve` が Chat の
  イベントを直接受ければ、エージェントは REST の外に入口を持つ。橋は
  Web UI と同じく REST の一つのクライアントで、問いの形も、turn の残り方も、
  範囲の効き方(0109)も、他のどの面から問うたときとも同じである。
- **`serve` の中に置かない。** 置けば、Chat のためだけに `serve` が
  Chat のトークンを受け、Chat の形を知ることになる。serve-ui が別の
  サービスである理由(ページと API の脅威モデルを分ける)と同じ理由で分ける。
  イメージは一つで、`--args=serve-chat` で動く。
- **本人として記録する。** イベントの `user.email` は Google が入れた
  ものである。橋はそれを `Ochakai-On-Behalf-Of: human:<email>` として
  渡し、`serve` は橋のサービスアカウントが `OCHAKAI_DELEGATING_CALLERS`
  にあるときだけ受け入れる。turn は `human:<email> via process:<橋>` で、
  書いた本人のものになる。
- **二つのイベントの形を読む。** Chat API のインタラクションイベントと、
  Google Workspace アドオンとして作った Chat アプリのイベント(同じ値が
  `chat` の下にある)。返事も来た形で返す。

## 3. secret が要らない理由

| 要るもの | どうやって |
|---|---|
| 呼んだのが Chat であること | Google が署名した ID トークン。audience は Chat に設定したエンドポイントの URL、email は `chat@system.gserviceaccount.com`(アドオンなら `service-<番号>@gcp-sa-gsuiteaddons…`)。橋が Google の公開鍵で確かめる。Cloud Run も invoker として確かめる |
| 問うたのが誰か | イベントの `user.email`(Google が入れた値)を、既存の委譲で渡す |
| `serve` に届くこと | 橋のサービスアカウントの ID トークン(メタデータサーバー) — serve-ui と同じ |
| 返事を書くこと | 同期の HTTP 応答。Chat API を呼ばないので、そのためのトークンも要らない |

**Chat のトークンは橋でも確かめる。** Cloud Run を非公開にして Chat
だけに invoker を与えれば、Cloud Run が既に確かめている。それでも橋が
確かめるのは、誤って `allUsers` にしたデプロイで、誰でも好きな本人の名で
問えるようにならないためである。確かめる audience は、Chat が呼んだ URL
そのものである。認証オーディエンスに「プロジェクト番号」を選んだ Chat
アプリ(自己署名の JWT)は受けない — 設定を一つに決め、確かめ方を一つにする。

## 4. Chat でしないこと

- **SQL を走らせない。** 本人のトークンが Chat には無い。サーバーは SQL を
  実行しない(0142 §4)ので、提案は SQL として見せ、Web UI で訊き直すよう
  書く。Chat のユーザー認可(OAuth)で本人のトークンを得るには OAuth
  クライアントの secret が要り、C2 に反する。
- **30 秒を超えて待たない。** 同期の返事は 30 秒までである。橋は 25 秒で
  打ち切り、Web UI で訊き直すよう答える。非同期に返すには Chat API を
  アプリとして呼ぶことになり、Cloud Run が応答のあとも CPU を持ち続ける
  設定か、キューが要る。いまは払わない。
- **スレッドの前の発言を読まない。** 一つのメッセージが一つの問いである。
  前の発言を読むには Chat API を呼ぶ必要がある(同じ理由で、いまは払わない)。
- **👍 / 👎 を Chat に置かない。** 判定は Web UI にある。カードのボタンは
  次の一件で足せばよい。

## 5. 面

- **REST・MCP・CLI・環境変数** — どの数も動かない。橋は既存の
  `POST /api/v1/agent`、既存の `Ochakai-On-Behalf-Of`・`Ochakai-Producer`、
  既存の `OCHAKAI_URL`・`OCHAKAI_DELEGATING_CALLERS` だけを使う。
- **`serve-chat` は数えない** — `serve`・`serve-ui` と同じく、バイナリの
  動かし方である([docs/surface.md](../surface.md) の「数えていないもの」)。
  運用者の払うものは、デプロイガイドの一節(§5e)と、任意のもう一つの
  サービスである。
- **Web UI** — 変わらない。

## 6. 退けた案

- **`serve` が Chat のイベントを直接受ける。** エージェントに第二の入口が
  でき(0145 §7)、`serve` が Chat の形と Chat のトークンを知る(§2)。
- **Chat API で非同期に返す、スレッドを読む。** §4 — CPU の常時確保か
  キューという新しい依存と引き換えで、いまの問いの多くは 25 秒で答える。
  見直す条件: 25 秒を超えて打ち切られる問いが、Chat の問いの無視できない
  割合になったとき。
- **Chat のユーザー認可で本人の SQL を走らせる。** OAuth クライアントの
  secret が要る(C2)。
- **Slack の橋。** Slack からの呼び出しを確かめるには署名用の secret が、
  返事を書くにはボットのトークンが要る。どちらも ochakai が持たないと
  決めているものである。Slack で問う人には、Claude のコネクタ(次の記録)
  を通る道を探す。
- **プロジェクト番号の audience(自己署名の JWT)も受ける。** 確かめ方が
  二つになり、設定の取り違えが「一方だけ効く」になる。一つに決める。
