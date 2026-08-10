---
type: Skill
title: Web UI スクリーンショットの再現手順の癖
description: 撮り直しで踏む二つの罠(ダークスキームと ICU ロケール)と、コミット前の目視規則
tags: [dogfood, runbook]
status: draft
sources:
  - id: contributing
    resource: CONTRIBUTING.md
    title: Web UI screenshots の節(ルート・高さ・縮小の全表)
generated:
    by: process:claude-code
    via: human:anonymous
    producer: claude-code/1.0
    at: "2026-08-10T02:37:12Z"
created_by: process:claude-code via human:anonymous using claude-code/1.0
---

docs/images/webui-*.png は examples/demo を実際に serve して撮る。
ルート・高さ・縮小コマンドの全表は CONTRIBUTING.md にある — ここに置く
のは、表を正しくなぞっても絵が狂う二つの罠と、最後の規則である。

- **ライトスキームは強制する。** ヘッドレス Chrome の既定はダーク。
  `Emulation.setEmulatedMedia` で明示する。
- **ロケールは DevTools プロトコルでしか直らない。** entry ページの日付
  は `toLocaleDateString()` が ICU の既定ロケールを読む。macOS では
  `--lang=en-US` も `LANG`/`LC_ALL` も効かず、`navigator.language` が
  en-US を名乗ったまま日付だけ `2026年7月27日` になる。
  `Emulation.setLocaleOverride` で上書きし、ページ内で
  `Intl.DateTimeFormat().resolvedOptions().locale` が `en-US` である
  ことを確かめる。
- **コミット前に全 PNG を読む。** 落ちたバックエンドは整った「502 Bad
  Gateway」カードとして描画され、一瞥ではコンテンツに見える。ダーク・
  日本語日付・ほぼ空・エラー表示のどれかがあれば撮り直す。
