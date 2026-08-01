# ochakai 設計ドキュメント 0011: 添付画像バイト列の GCS 移行

Status: Superseded by [0046](0046-bundle-address-space.md)
Date: 2026-07-18

添付画像のバイト列を PostgreSQL のコンテンツアドレス blob 行から GCS
オブジェクトへ移す移行路を開通させた(`OCHAKAI_GCS_BUCKET` 未設定なら
従来どおり bytea、認証は ADC、書き込みは create-only)。バイト列を GCS に
置くという判断だけが 0046 に残る。全文は Superseded 直前のコミット
[428fbf0](https://github.com/na0fu3y/ochakai/blob/428fbf070d09d6f6b3a367786ec019211ca6a8c4/docs/design/0011-gcs-attachment-storage.md)
にある。
