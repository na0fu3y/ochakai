# ochakai 設計ドキュメント 0001: 全体アーキテクチャ

Status: Superseded by [0081](0081-what-ochakai-is-and-what-it-refuses-to-hold.md)
Date: 2026-07-14

LLM を内蔵せず SQL を実行しないナレッジストアという中核を置き、Go 単一
バイナリ + PostgreSQL 一本、共通エンベロープと provenance、双方向の書き戻し
ループ、利用テレメトリを決めた。先行事例と競合の日付入り調査(§2)も含めた
全文は Superseded 直前のコミット
[49bd267](https://github.com/na0fu3y/ochakai/blob/49bd267c33efef448be0c15e883527cd421e6a0e/docs/design/0001-architecture.md)
にある。
