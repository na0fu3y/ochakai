# ochakai 設計ドキュメント 0121: 経路ごとに、読むヘッダが違う

Status: Accepted(2026-08-23)。[0086](0086-a-second-way-to-say-who-is-calling.md) に
**自分で検証するデプロイが資格情報をどのヘッダで受け取るか**を足し、
[0065](0065-identity-and-provenance.md) §2 の「`X-Serverless-Authorization`
を優先して読む」がそのうち Cloud Run 経路だけの規則であることを言う。
検証の内容も、記録される名前も、secret-zero も動かない。
[0119](0119-an-operated-fleet-is-deployments-or-directories.md) §5 の
宿題の一番目に要る前提であり、それ自体は姿勢の決定ではない
Date: 2026-08-23

## 1. 何が壊れていたか

**OIDC 発行者を名指したデプロイを Cloud Run IAM の後ろに置くと、
すべてのリクエストが 401 になる。** リクエストの中に有効な資格情報が
入ったまま、一度も読まれない。

再現は一行で言える。0065 §2 の規則が

> `X-Serverless-Authorization`(両方あるとき Cloud Run が検証するのは
> こちら)を優先して読み、無ければ `Authorization` を読む

であり、この規則は 0086 が第二の経路を足したときも**そのまま両方の経路に
掛かっていた**。だから OIDC のデプロイは、Cloud Run が前段で検証して
転送した Google のトークン(署名は
`SIGNATURE_REMOVED_BY_GOOGLE` に置き換わっている)を拾い、
それを**自分の発行者の鍵で検証しようとして**落ちる。

```
401 {"code":"invalid","error":"auth: token signature does not verify
     against https://issuer.example's published keys"}
```

**暗号の失敗に見えて、ヘッダが一つずれているだけである。** 呼び出し元の
本物のトークンは同じリクエストの `Authorization` に入っている。

## 2. それは Google の二ヘッダ分割が意図どおり働いた形である

この構成は珍しい要求ではない。**Cloud Run IAM を満たしつつ、アプリケー
ション自身のトークンも運ぶ**クライアントのために、Google は最初から
ヘッダを二本に分けている — Google のトークンを
`X-Serverless-Authorization` に、アプリのトークンを `Authorization` に
置く。ochakai 自身の `serve-ui` がその形で書かれている
(`newServiceProxy`)。

つまり 0086 の後、**プラットフォームが検査するヘッダとアプリケーションが
検査するヘッダは別々にあった**のに、ochakai はどちらの経路でも前者を先に
読んでいた。

## 3. 決定: 自分で検証するデプロイは `Authorization` だけを読む

- **Cloud Run 経路(検証器なし)は今までどおり** `X-Serverless-Authorization`
  を優先する。**この優先には理由がある** — 二本のうち Google が検証したのは
  そちらだけなので、もう一方を読めば、認証された呼び出し元が
  `Authorization` に他人の名前を入れて**その人として記録される**。
- **OIDC 経路(検証器あり)は `Authorization` だけを読む。**
  上の理由がここには無い: **どちらのヘッダも前段では検証されておらず**、
  渡されたトークンを検証するのは ochakai 自身なので、防ぐべき詐称が
  存在しない。優先が実際にしていたのは、呼び出し元の本物の資格情報を、
  この発行者が発行していないトークンで**覆い隠す**ことだけだった。

**`X-Serverless-Authorization` しか無いリクエストは、そう言って断る。**
黙って落ちれば §1 の「署名が合わない」になり、それは暗号の問題では
ないものを暗号の言葉で報告する。[0117](0117-a-person-recorded-as-a-process-says-so.md)
が置いた形と同じで、**見分けられるものは言う**。

```
401 this deployment verifies its own tokens and reads Authorization;
    the credential arrived in X-Serverless-Authorization, which is
    Cloud Run's header for the check it makes in front of the process
```

## 4. 何が変わらないか

- **検証の中身**(署名・発行者・audience・有効期限)も、**記録される名前**
  (0086 §4、0117)も、**委譲**(0065 §3)も一字も動かない。
- **secret は増えない。** ヘッダを一本読まなくなるだけである。
- **姿勢は増えない。** この記録は「公開 invoke で自分を検証するデプロイ」を
  **綴っていない** — それは 0119 §5 の一番目で、deploy ガイドの
  `allUsers` 禁止と [SECURITY.md](../../SECURITY.md) の脅威モデル、そして
  [0066](0066-four-postures-one-word.md) §7 が断ったレート制限を動かす、
  別の決定である。**ここで直したのは、その決定を測る前から壊れていた
  構成である。**

## 5. 直した構成は、それ自体で使える

**OIDC × Cloud Run IAM は二重の関門であって、公開の前段階ではない。**
到達できる相手を Cloud Run IAM が決め、その中で**誰であるか**を運用者の
発行者が決める — 組織の IdP に identity を寄せたいが、サービスを公開に
する気は無いデプロイの形である。0086 は「Google Cloud の外」のために
書かれたが、決めたのは**発行者を名指したデプロイは自分で検証する**こと
であって、どこで動くかではない。

## 6. 面

**一つも動かない。** REST も MCP も CLI も Web UI も、環境変数も語彙も
同じである。動いたのは、**設定済みのデプロイがどのヘッダを読むか**で、
それは [docs/surface.md](../surface.md) の HEADER が数える名前の一覧を
変えない — 二本とも、両経路を合わせれば今までどおり読まれる。

契約も変わらない。401 は 0086 §6 が全操作に宣言済みで、増えたのは
その中の文である。

## 7. 採らなかった案

- **OIDC 経路で `X-Serverless-Authorization` を fallback として読むこと。**
  §3 のとおり、読めば「Google の検査のために作られたトークンを、この
  デプロイの発行者の鍵で検証する」ことになり、答えは必ず 401 である。
  **必ず失敗する経路は経路ではない。**
- **両方のヘッダを試すこと。** 二つの資格情報のうち通ったほうを採る、は
  「どちらとして記録されたか」がリクエストごとに変わることであり、
  provenance の側から見て最も避けたい形である。
- **Cloud Run 経路も `Authorization` だけにすること。** できない —
  §3 の一つ目の理由が現に効いている。**二つの経路が違う答えを持つことは
  意図であって不統一ではない**(0086 §5 が「二度目の検証は買うものが
  無い」と書いたのと同じ性質である)。
- **起動時に断ること**(検証器があるのに Cloud Run の後ろにいる、を検出)。
  検出できない: プロセスからは自分が IAM の後ろにいるかどうかが見えず、
  見えるのは届いたリクエストのヘッダだけである。そしてこの記録の後、
  その構成は**正しい構成**である。
