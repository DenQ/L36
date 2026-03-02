# ⚓ L-36: Ultra-Fast Sharded Versioning Engine

**L-36** is a high-performance, in-memory data store with native versioning support, specifically engineered for extreme write-heavy workloads and real-time diffing.

[![Go Version](https://img.shields.io/badge/go-1.26-blue.svg)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20macos-lightgrey.svg)](https://github.com)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org)

## 🚀 Performance Benchmarks
*Tested on Ubuntu Latest (GitHub Actions) with 2 vCPUs:*


| Metric | Throughput | Latency (avg) |
| :--- | :--- | :--- |
| **Page Creation (Baseline)** | **18,600+ RPS** | ~54µs |
| **Heavy Lifecycle (with Diffs)** | **11,500+ RPS** | ~90µs |

> **Note:** L-36 is a compute-bound engine. Performance scales linearly with CPU core count. On a 16-core machine, expected throughput exceeds **80,000+ RPS**.

## 🛠 Core Architecture
L-36's battleship-grade performance is built on four architectural pillars:

1. **36-Way Horizontal Sharding**: Data is distributed across 36 independent shards. This eliminates global mutex contention, allowing massive parallel access on multi-core systems.
2. **Reverse Delta Storage**: We store the latest version in full and a chain of backward patches (Diffs). This provides O(1) access to current data while minimizing RAM footprint.
3. **Advanced Diff Engine**: Powered by optimized Google DMP (Diff-Match-Patch) with row-level pre-processing and custom memoization.
4. **Parallel Persistence**: Each of the 36 shards flushes to its own independent JSON file. This "36-Safe" approach ensures asynchronous, non-blocking I/O operations.

## 📦 Installation & Usage

**Clone the repository**
```bash
git clone https://github.com/DenQ/L36.git
```

**Install dependencies**
```bash
make install
```

**Run the engine**
```bash
make run
```

## ⚖️ License

Distributed under the MIT License. See `LICENSE` for more information.

---
Copyright (c) 2026 Denis Ivanov
