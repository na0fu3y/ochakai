# ochakai — データ作業のためのチームのナレッジベース

<!--
この節をあなたのプロジェクトの CLAUDE.md にコピーする。CLI をサーバーに
向けるのはマシンごとに一度でよい — 認証はあなたの gcloud ログインか
サービスアカウントの ADC であり、設定するトークンは無い:

    ochakai use https://ochakai-<hash>.run.app

(あるいは OCHAKAI_URL を設定する。こちらが優先される — CI で便利。)
`ochakai whoami` が、どのサーバーに誰として向いていて、届いているかを
言う。

ナレッジベースがまだ空で、concept の形を先に見たいなら、ochakai の
リポジトリで `ochakai import examples/demo` を**捨ててよいサーバー**に
対して走らせる — 9 型のうち 8 型の見本が 18 concept 入っている。SQL は
実在の公開データセットに対して動くが、判断とチームは作り物なので、
実際に使うナレッジベースには入れない
([examples/README.md](../README.md))。
-->

ochakai はメトリクスの定義、attested computation(sanctioned な SQL。
検証済みの golden query を含む)、解釈のナレッジ(メトリクスの読み方)、
用語、テーブルカタログの項目を持つ。分析 SQL を書く前にここを検索し、
学んだことを書き戻すこと。

- `ochakai search "<question or keyword>" [--type '<Type>'] [--trust human-reviewed]`
  — データの問いはここから始める。1 行 1 ヒット: uri、status、title。
  検証済みの concept は信用してよい(`--json` の各ヒットが `trust` と
  `verified_at` を持つ)。`draft` の concept は provenance で判断する —
  誰が書いたかはヒットには無いので、下の get の stderr で読む。読む価値の
  ある hit は下の get で全文を取る。
- `ochakai get <id>` — concept の全文を markdown(YAML frontmatter +
  本文)で。stderr の `linked from:` 行は、この concept を本文から指す
  concept — metric の読み方を言う insight はそこに出るので、数字を信じる
  前にそれも get すること。本文の markdown リンクを辿ると関連する
  concept に行ける —
  他の concept のパスへのリンク `[revenue](/metrics/revenue.md)` が
  concept どうしの関係であり、それを書くことが関係を作ることである。
  stderr がファイル(ダッシュボードのスクリーンショット、ER 図)を挙げた
  なら `ochakai get <id> --download <dir>` で取得し、本文の画像参照が
  その問いに効くときは保存されたファイルを Read すること。
- `ochakai put <id>/<name> -f <file>` — バンドルにファイルを置く
  (任意のバイト列。パスがその住所である)。concept の本文からリンク
  すること — `![name](<id last segment>/<name>)` — そうすればキャプション
  が検索でき、ファイルに持ち主ができる。ファイルを見て何か分かったら、
  それも `ochakai put` で本文に書くこと — ピクセルに閉じ込められた
  ナレッジは検索から見えない。
- `ochakai report <id> worked|failed [--note "what went wrong"]`
  — ナレッジに基づいて動いた後(attested computation を走らせた、
  メトリクス定義から自分で書いた SQL を走らせた)、結果が実際に正しかった
  かを報告する。`failed` の報告は検証済み concept を再検証の対象に立て、
  次のエージェントが古びた concept を黙って信じないようにする。検証済みの
  concept に導かれて間違った数字に行き着いたときは、必ず `failed` を
  報告すること。
- `ochakai put <id> -f entry.md` — 学んだことを書き戻す(`get` が印字
  するのと同じ OKF markdown、または JSON。`ochakai put -h` を見よ)。
  id は concept のパス(`queries/sales/monthly-revenue`)であり、引数と
  して渡す: OKF 文書は id を持たないからである。concept は `draft` から
  始まり、あなたの identity が provenance として自動的に記録される。
- `ochakai export <dir>` — ナレッジベース全体を markdown としてスナップ
  ショットする。`ochakai import <dir>` はバンドルを読み戻す(どの OKF
  バンドルでもよい)。

**答えるときは、どの concept から来たかを言う。** ナレッジベースから
持ってきた定義・数え方・SQL は、**concept の id と、それが人に確認された
ものかどうか**を添えて答えに書くこと —「売上は税と送料を除く
(`metrics/revenue`、2026-07-14 に human-reviewed)」のように。値は
`ochakai get` が stderr に出す一行(`verified by … on …; created by …`)
にあり、`ochakai search --json` の各ヒットは `trust` と `verified_at` を
持つ。

理由は二つある。**読んだ人が確かめられる** — 引用が無ければ、人が検証
した定義と、あなたがその場で組み立てた推測が、答えの中で同じ顔をして
並ぶ。そして **直せる場所が分かる**: 定義が古いと分かったとき人が開くのは
その concept であり、`ochakai report <id> failed` を打てるのも id を
知っているときだけである。引用は、上の report の習慣を**人間の側にも**
使えるようにする一行である。

**確認されていないものは、そう言う。** `draft` の concept を引くのは
構わない — 黙って引くのが間違いである。「まだ draft」と書けば、読んだ人は
その一行を自分で確かめるかどうかを決められる。

ここに挙げた以外の型を使ってよいし(任意のスラグが通る — 例
`runbook/…`)、関連するナレッジをまとめるために id は階層的でよい
(`queries/sales/monthly-revenue`)。

**型は concept が何を持つかで選ぶ。** 推奨の 9 型それぞれに何を書くかは
`ochakai put -h` が一行ずつ言う — 閉じた集合ではないので、自分の型
(`runbook` など)を書いてもよい。迷ったら同じ型の concept を一つ読んで
形を真似ること: `ochakai search "<話題>" --type '<Type>'` で見つけ、
`ochakai get <id>` で全文を出す。

自分が書いたクエリが正しく、しかも役に立つと分かったら
`type: Attested Computation` として保存すること。OKF の frontmatter は
平坦なので、自分のキー(その SQL が答える問いを運ぶ `question` など)は
spec が定義するキーの**隣**に置き、その下に入れ子にしない。後で人が
`ochakai verify` で確かめる。
