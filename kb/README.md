# kb/ — ochakai 自身のナレッジベース

ochakai の開発・運用ナレッジを OKF v0.2 バンドルとして持つ、この
プロジェクト自身の dogfooding である。スクリプトの入ったフォルダでは
なくナレッジとして出荷される点は
[examples/bigquery-catalog](../examples/bigquery-catalog) と同じで、
ループの回し方はバンドル自身の concept が説明する。

## 使い方

ローカルインスタンス([deploy/compose.yaml](../deploy/compose.yaml))を
立てて読み込む:

```sh
docker compose -f deploy/compose.yaml up -d
ochakai use http://localhost:8080
ochakai import kb/bundle
```

そこから先 — 誰がどの identity で書くか、レビューの回し方、ここへの
書き戻し — は
[skills/run-the-dogfood-loop](bundle/skills/run-the-dogfood-loop.md) と
[policies/ai-human-identity](bundle/policies/ai-human-identity.md) に
ある。

二つだけ、バンドルの外に書いておく価値がある:

- **trust はこのディレクトリではなくデータベースに住む。** 検証台帳は
  export/import を往復しない — import は文書の verified キーを
  「インポートを実行した者の検証」として記録し直す(review gate、
  設計ドキュメント 0043 §3.2)。だから import を回すのは人間である。
- **ここにある文書は draft として入る。** AI が起草したものは、人が
  `ochakai verify` するまで unverified のままでよい。それが正しい姿で
  ある。
