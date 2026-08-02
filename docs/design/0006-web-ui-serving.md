# ochakai 設計ドキュメント 0006: Web UI の二つの配信経路

Status: Superseded by [0072](0072-the-web-ui-serves-and-edits-documents.md)
Date: 2026-07-17

自己完結一ファイルの UI を、認証モデルの異なる二経路(`ochakai ui` と
`ochakai serve-ui`)で配信すると決めた — ページは認証を知らず、危険な
組み合わせはコマンドを分けることで書けなくなる。全文は Superseded 直前のコミット
[f2bab02](https://github.com/na0fu3y/ochakai/blob/f2bab028f8553e6a2e9134613d638d6224683bac/docs/design/0006-web-ui-serving.md)
にある。
