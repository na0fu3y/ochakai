# ochakai 設計ドキュメント 0014: フォルダブラウズ

Status: Superseded by [0046](0046-bundle-address-space.md)
Date: 2026-07-18

エントリの ID 階層を 1 階層ずつ辿る `GET /api/v1/browse` と、Web UI の
prefix によるツリー閲覧を導入した。1 階層ずつ返す・1000 件で打ち切る・
利用を記録しないという判断は 0046 でも維持され、面は生成される
`index.md` に吸収される。全文は Superseded 直前のコミット
[428fbf0](https://github.com/na0fu3y/ochakai/blob/428fbf070d09d6f6b3a367786ec019211ca6a8c4/docs/design/0014-folder-browse.md)
にある。
