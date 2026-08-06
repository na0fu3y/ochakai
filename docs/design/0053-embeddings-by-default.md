# ochakai 設計ドキュメント 0053: 埋め込みは既定であり、ベクトル空間は捨ててよい

Status: Superseded by [0080](0080-search-and-how-a-deployment-embeds.md)
(0073 経由)
Date: 2026-07-29

Google Cloud の上ではプロジェクトをメタデータサーバから発見して意味的検索を
既定で有効にし、使えるかは設定ではなく IAM が決めると定めた。次元変更時の
手作業の `DROP TABLE` は廃し、**ベクトルは導出物である**として製品が作り直す。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0053-embeddings-by-default.md)
にある。
