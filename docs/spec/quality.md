# 実装の品質ゲート

**規約を信じず、外から不変条件を読む。** 型や抽象を足すのではなく、Go に
ある道具で、壊れたら落ちる検査を置く。CI と手元は同じ `scripts/check` を
走らせるので、二つはずれない。

## 走らせるもの

```sh
scripts/check          # 全部。`scripts/check core` が CI のテストのジョブ
scripts/check --db     # 使い捨ての PostgreSQL で store のテストも
```

gofmt・`go vet`・`go fix -diff`・`go test -race`・静的ビルド・Web UI の
`node --test`・golangci-lint・zizmor(GitHub Actions)・govulncheck。

## 静的解析

`.golangci.yml` の選び方は一つ — **きれいな木で 0 件であること**。指摘が
出たら常に「新しいコードが何かした」を意味し、好みを意味しない。恒常的に
ノイズを出す検査は、どれだけ有名でも入れない(`nolint` の乱発に化ける)。

中心は `exhaustive`(直和型の網羅。`default` を書いた `switch` は網羅を
表明済みとみなす)で、ほかに `errorlint`・`staticcheck`・`bodyclose`・
`nilerr`・`unconvert` / `unparam` を使う。

## 契約と性質

- **契約テスト**: `internal/restapi` のテストを通る要求と応答を
  [api/openapi.yaml](../../api/openapi.yaml) に照合する。生成器は使わず、
  手書きのハンドラと契約をテストで結ぶ。凍結の指紋も検査する
  ([rest-stability.md](rest-stability.md))。
- **性質テスト**: Go 標準の fuzz で、信頼できない入力を食う OKF のパーサ、
  id とリンクの導出、export → import の往復の同一性を試す。

## 文書と数を、実物と突き合わせる

散文はコンパイルされないので、読み返す検査を置く。

- **表面の数**: [surface.md](../surface.md) の九つの節と天井を、ビルドから
  数え直す(`surface_test.go`・`TestCeilings`)。
- **設定の名前**: 読む環境変数の一覧をソースの `os.Getenv` から読み戻す。
- **CLI の参照**: `docs/cli.md` はコマンドの `-h` から生成し、ずれたら落ちる。
- **マニュアルのリンク**と、存在しないコマンドを名指していないこと。
- **退役した綴り**が、書き直せる文書に残っていないこと。
- **決定ログの形**(番号・題・日付・領域)と長さ(`DECISION-LINES`)。
- **画面の日本語**の規則(`copy.test.js`)。
- 権限の反射テスト: すべての書き込みメソッドを範囲の外の id で呼び、
  拒まれることを確かめる。新しい書き込みを足しても、書き忘れた検査が
  ここで見つかる。

## 採らないもの

- 検証済みの型を全面に使う設計(Go ではメソッドとゼロ値の問題を買う)。
- NilAway、形式手法(この規模では割に合わない)。
- 数の天井を散文の量に置くこと(名前の数には置く)。

## 経緯

設計記録 [0035](../design/0035-verifiability.md)、
手順は [CONTRIBUTING.md](../../CONTRIBUTING.md)。
