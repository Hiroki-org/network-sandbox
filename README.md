# Network Sandbox

分散システムの負荷分散をリアルタイムで可視化・学習するためのサンドボックス環境です。

![Architecture](docs/architecture.png)

## 🎯 概要

Network Sandbox は、ロードバランサー、マルチ言語ワーカー、モニタリングスタックを組み合わせた教育・実験用プラットフォームです。様々な負荷分散アルゴリズムの動作を視覚的に理解し、カスタム実装を簡単にテストできます。

## ✨ 機能

- **4種類の負荷分散アルゴリズム**: ラウンドロビン、最小接続、重み付け、ランダム
- **マルチ言語ワーカー**: Go, Rust, Python 実装
- **リアルタイムUI**: WebSocket による即時更新
- **サーキットブレーカー**: 障害時の自動切り離し
- **Prometheus/Grafana**: フルモニタリングスタック
- **スワップ可能アーキテクチャ**: カスタム実装との簡単な入れ替え

## 🚀 クイックスタート

```bash
# リポジトリをクローン
git clone https://github.com/Hiroki-org/network-sandbox.git
cd network-sandbox

# 起動
docker-compose up -d

# アクセス
# - クライアントUI: http://localhost:3000
# - Grafana: http://localhost:3001 (admin/admin123)
# - Prometheus: http://localhost:9090
```

## 📁 プロジェクト構造

```
network-sandbox/
├── client/                    # React フロントエンド
├── load-balancer/            # Go ロードバランサー
├── workers/
│   ├── go/                   # Go ワーカー
│   ├── rust/                 # Rust ワーカー
│   └── python/               # Python ワーカー
├── grafana/                  # Grafana ダッシュボード設定
├── prometheus/               # Prometheus 設定
├── docker-compose.yml        # サービス定義
├── docker-compose.override.yml.example  # カスタマイズ例
└── .env.example              # 環境変数テンプレート
```

## 🔧 カスタマイズ

### 環境変数

```bash
cp .env.example .env
# .env を編集してカスタマイズ
```

### カスタム実装への入れ替え

このプロジェクトは **スワップ可能なアーキテクチャ** を採用しています。自作のロードバランサーやワーカーを簡単に組み込めます。

#### 方法1: docker-compose.override.yml を使用

```bash
cp docker-compose.override.yml.example docker-compose.override.yml
```

```yaml
# docker-compose.override.yml
services:
  # 自作ロードバランサーに差し替え
  load-balancer:
    build:
      context: ./my-custom-load-balancer
      dockerfile: Dockerfile
    environment:
      - MY_CUSTOM_CONFIG=value

  # 自作ワーカーを追加
  my-worker:
    build:
      context: ./my-workers/custom
      dockerfile: Dockerfile
    environment:
      - PORT=8090
      - WORKER_NAME=my-worker-1
      - WORKER_COLOR=#FF00FF
```

#### 方法2: 環境変数でイメージを指定

```bash
# .env
LB_IMAGE=your-registry/my-load-balancer:latest
WORKER_GO_IMAGE=your-registry/my-go-worker:latest
```

### カスタムワーカーの作成

[Custom Worker Guide](workers/CUSTOM_WORKER_GUIDE.md) を参照してください。

必須エンドポイント:

- `GET /health` - ヘルスチェック
- `POST /task` - タスク処理
- `GET /metrics` - Prometheus メトリクス
- `GET /status` - ステータス情報

## 📊 モニタリング

### Grafana ダッシュボード

http://localhost:3001 でアクセス:

- **Load Balancer Overview**: リクエスト数、レイテンシ、アルゴリズム状態
- **Worker Health**: 各ワーカーの負荷、キュー深度、ヘルス状態

### カスタムメトリクス

ロードバランサー:

- `lb_requests_total` - 総リクエスト数
- `lb_request_duration_ms` - レイテンシ分布
- `lb_worker_health` - ワーカー健全性

ワーカー:

- `worker_requests_total` - 処理リクエスト数
- `worker_active_requests` - アクティブリクエスト数
- `worker_processing_time_ms` - 処理時間

## 🎮 UI コントロール

クライアントUI (http://localhost:3000) で以下を制御できます:

- **負荷生成**: リクエストレートとタスク重み調整
- **アルゴリズム選択**: リアルタイムで切り替え
- **統計表示**: 成功/失敗数、平均応答時間
- **ワーカー状態**: 各ワーカーの負荷とヘルス

## 🧪 負荷分散アルゴリズム

| アルゴリズム      | 説明                     | 適用シーン           |
| ----------------- | ------------------------ | -------------------- |
| Round Robin       | 順番に振り分け           | 均一負荷のワーカー   |
| Least Connections | 最も空いているワーカーへ | 可変処理時間         |
| Weighted          | 重みに基づいて振り分け   | 異なる性能のワーカー |
| Random            | ランダム選択             | シンプルな分散       |

## 🐛 トラブルシューティング

### コンテナが起動しない

```bash
# ログを確認
docker-compose logs -f load-balancer
docker-compose logs -f worker-go-1

# 再ビルド
docker-compose build --no-cache
docker-compose up -d
```

### メトリクスが表示されない

1. Prometheus ターゲット確認: http://localhost:9090/targets
2. ワーカーメトリクス確認: `curl http://localhost:8081/metrics`

## 📝 ライセンス

MIT License
