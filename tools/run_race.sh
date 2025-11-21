#!/usr/bin/env bash
# 说明：在指定目录顺序运行 `go test -race` 100 次并记录日志，可配合 nohup 后台执行。
# 背景：批量运行有助于复现偶发并发/持久化问题，便于定位修复。
# 设计取舍：脚本不强制自后台化，保留由调用者通过 `nohup ... &` 或追加 `&` 控制后台运行；
# 顺序执行避免并发带来的额外干扰；使用独立日志文件便于后续分析。

set -uo pipefail

# 目标目录（默认为 raft 包目录，基于项目根的相对路径）
TARGET_DIR="../code/src/raft"
# 运行次数，默认 500，可通过环境变量覆盖：ITERATIONS=1000
ITERATIONS="${ITERATIONS:-100}"
# 日志目录改为脚本所在目录（可通过环境变量覆盖：LOG_DIR=/tmp/raft_logs）
# 为什么：将日志与脚本放在一起，便于定位与归档；避免依赖目标目录结构。
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# 切到脚本目录，后续路径操作仍可使用相对路径，但日志文件使用脚本目录的绝对路径，
# 以便在切换到测试目录后，tee 仍写入脚本目录中的同一日志文件。
cd "$SCRIPT_DIR" || { echo "无法进入脚本目录: $SCRIPT_DIR"; exit 1; }
LOG_DIR="${LOG_DIR:-$SCRIPT_DIR}"
TS="$(date '+%Y%m%d_%H%M%S')"
LOG_FILE="$LOG_DIR/race_${ITERATIONS}_runs_$TS.log"
SUMMARY_FILE="$LOG_DIR/race_${ITERATIONS}_runs_$TS.summary"

mkdir -p "$LOG_DIR"

success=0
fail=0
# 统计耗时（单位：秒），仅使用秒级以兼容 macOS/BSD date
total_duration=0
max_duration=-1
min_duration=-1

echo "开始批量测试: 目录=$TARGET_DIR 次数=$ITERATIONS 时间=$TS" | tee -a "$LOG_FILE"

if ! cd "$TARGET_DIR"; then
  echo "目录不存在或不可访问: $TARGET_DIR" | tee -a "$LOG_FILE"
  exit 1
fi

for ((i=1; i<=ITERATIONS; i++)); do
  echo "===== [$(date '+%F %T')] 第 $i 次 go test -race =====" | tee -a "$LOG_FILE"
  start_ts=$(date +%s)
  if go test -race 2>&1 | tee -a "$LOG_FILE"; then
    end_ts=$(date +%s)
    duration=$((end_ts - start_ts))
    echo "[第 $i 次] 耗时: ${duration}s" | tee -a "$LOG_FILE"
    echo "[第 $i 次] 结果: 成功" | tee -a "$LOG_FILE"
    ((success++))
  else
    # 记录 go test 的退出码（而非 tee 的退出码）
    rc=${PIPESTATUS[0]}
    end_ts=$(date +%s)
    duration=$((end_ts - start_ts))
    echo "[第 $i 次] 耗时: ${duration}s (失败)" | tee -a "$LOG_FILE"
    echo "[第 $i 次] 结果: 失败 (exit $rc)" | tee -a "$LOG_FILE"
    ((fail++))
  fi

  # 更新耗时统计：总时长、最短、最长
  total_duration=$((total_duration + duration))
  if ((max_duration < 0 || duration > max_duration)); then
    max_duration=$duration
  fi
  if ((min_duration < 0 || duration < min_duration)); then
    min_duration=$duration
  fi
done

avg_duration=$(( ITERATIONS > 0 ? total_duration / ITERATIONS : 0 ))
echo "===== 完成: 成功=$success 失败=$fail 总计=$ITERATIONS 平均耗时=${avg_duration}s 最长=${max_duration}s 最短=${min_duration}s =====" | tee -a "$LOG_FILE"
printf "success=%d\nfail=%d\ntotal=%d\nlog_file=%s\navg_duration_sec=%d\nmax_duration_sec=%d\nmin_duration_sec=%d\n" \
  "$success" "$fail" "$ITERATIONS" "$LOG_FILE" "$avg_duration" "$max_duration" "$min_duration" | tee -a "$LOG_FILE" > "$SUMMARY_FILE"

echo "日志: $LOG_FILE" | tee -a "$LOG_FILE"
echo "摘要: $SUMMARY_FILE" | tee -a "$LOG_FILE"

# 使用建议：
# 后台运行（推荐）
# nohup bash tools/run_race.sh >/dev/null 2>&1 &
# 自定义参数：
# ITERATIONS=20 LOG_DIR=/tmp/raft_logs nohup bash "$TARGET_DIR/run_race.sh" >/dev/null 2>&1 &
