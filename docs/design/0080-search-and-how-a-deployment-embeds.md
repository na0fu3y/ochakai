# ochakai 設計ドキュメント 0080: 検索が何を融合し、このデプロイがどう埋め込むか

Status: Superseded by [0147](0147-search-and-the-default-a-base-was-made-with.md)
Date: 2026-08-07

Google Cloud の上では埋め込みを既定にし、プロジェクトとリージョンを
メタデータサーバから読んで、デプロイのリージョンで埋め込むと決めた。
設定は `OCHAKAI_EMBEDDINGS` 一語、幅はモデルごとの定数(768)。0147 が
節の番号ごと受け、新しいベースの既定だけを `global` の 2 に移した。全文は
Superseded 直前のコミット
[906a7f3](https://github.com/na0fu3y/ochakai/blob/906a7f3acbe9df05400869bb2adc3d720e15e893/docs/design/0080-search-and-how-a-deployment-embeds.md)
にある。
