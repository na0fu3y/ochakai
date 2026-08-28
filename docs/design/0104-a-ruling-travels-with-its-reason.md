# ochakai 設計ドキュメント 0104: 裁定は理由ごと運ばれる

Status: Superseded by [0135](0135-a-rejection-is-a-deletion.md)
Date: 2026-08-15

却下の理由(`--note`)が読める場所を三つ増やした: export の frontmatter に
`rejected_note` を出し、`ochakai get` が誰がいつなぜ却下したかを stderr に
言い、`search` / `list` の結果に却下分が混じるとき stderr が一行そう言う。
0135 が却下を削除にしたので、三つとも置き場ごと無くなった — 理由は
OKF SPEC §9 の `log.md` が運ぶ。全文は Superseded 直前のコミット
[e305197](https://github.com/na0fu3y/ochakai/blob/e305197720fc9097596e951ae5be12a45c7d7cc9/docs/design/0104-a-ruling-travels-with-its-reason.md)
にある。
