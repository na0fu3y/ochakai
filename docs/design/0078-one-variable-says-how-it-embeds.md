# ochakai 設計ドキュメント 0078: このデプロイがどう埋め込むかを、一つの変数で言う

Status: Superseded by [0080](0080-search-and-how-a-deployment-embeds.md)
Date: 2026-08-02

埋め込みの五つの設定を `OCHAKAI_EMBEDDINGS` 一語に畳んだ — 未設定・`on`・
`off`・Vertex AI のモデル resource name の四つだけを取り、それ以外は起動
エラーにする。幅はデプロイの設定ではなくモデルごとの定数になった。全文は
Superseded 直前のコミット
[49bd267](https://github.com/na0fu3y/ochakai/blob/49bd267c33efef448be0c15e883527cd421e6a0e/docs/design/0078-one-variable-says-how-it-embeds.md)
にある。
