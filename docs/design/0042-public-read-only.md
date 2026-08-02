# ochakai 設計ドキュメント 0042: 公開読み取り専用という姿勢

Status: Superseded by [0066](0066-four-postures-one-word.md)
Date: 2026-07-27

誰でも到達できるデプロイのために **identity を一切読まない**姿勢を足すと
決めた — トークンも委譲ヘッダも見ず、全員 anonymous、401 を返さない。
書き込まないことが provenance を読まないことを正当化するので、read-only を
含意する。全文は Superseded 直前のコミット
[2bc9c77](https://github.com/na0fu3y/ochakai/blob/2bc9c77b487def57ad1b68847f138489b1a1edef/docs/design/0042-public-read-only.md)
にある。
