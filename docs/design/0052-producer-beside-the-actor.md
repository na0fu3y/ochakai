# ochakai 設計ドキュメント 0052: 名乗りは actor の隣に置く — SPEC §7 の `<producer>/<version>`

Status: Superseded by [0065](0065-identity-and-provenance.md)
Date: 2026-07-29

呼び出し元の自称(`Ochakai-Producer` / MCP の `clientInfo` /
`OCHAKAI_PRODUCER`)を、actor の**代わり**ではなく**隣**の第四フィールド
`Actor.producer` に記録すると決めた — 隣に置くことが、自称を認めてよい
条件そのものである。全文は Superseded 直前のコミット
[2bc9c77](https://github.com/na0fu3y/ochakai/blob/2bc9c77b487def57ad1b68847f138489b1a1edef/docs/design/0052-producer-beside-the-actor.md)
にある。
