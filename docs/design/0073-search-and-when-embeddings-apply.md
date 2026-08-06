# ochakai 設計ドキュメント 0073: 検索が何を融合し、埋め込みがいつ効くか

Status: Superseded by [0080](0080-search-and-how-a-deployment-embeds.md)
Date: 2026-08-03

Google Cloud の上では埋め込みを既定にし、使えるかを決めるのは設定ではなく
IAM だと決めた。検索は字句・concept のベクトル・ファイルのベクトルの三本を
RRF で融合し、ベクトルは導出物なので次元の変更は製品が行う。全文は
Superseded 直前のコミット
[49bd267](https://github.com/na0fu3y/ochakai/blob/49bd267c33efef448be0c15e883527cd421e6a0e/docs/design/0073-search-and-when-embeddings-apply.md)
にある。
