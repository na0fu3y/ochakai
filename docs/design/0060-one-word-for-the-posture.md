# ochakai 設計ドキュメント 0060: 姿勢は一語で言う

Status: Superseded by [0066](0066-four-postures-one-word.md)
Date: 2026-07-30

三つのブール(`OCHAKAI_READ_ONLY` / `OCHAKAI_PUBLIC_READ_ONLY` /
`OCHAKAI_INSECURE_DEV`)を `OCHAKAI_MODE` 一本に畳むと決めた
— **BREAKING**。書けた八通りのうち意味を持つのは二軸四象限の四つだけで、
差を埋めていた規則三つは消えるのではなく書き下せなくなる。全文は
Superseded 直前のコミット
[2bc9c77](https://github.com/na0fu3y/ochakai/blob/2bc9c77b487def57ad1b68847f138489b1a1edef/docs/design/0060-one-word-for-the-posture.md)
にある。
