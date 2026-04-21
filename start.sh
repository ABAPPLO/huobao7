#!/bin/bash
# 火宝短剧 一键启动脚本
# 用法:
#   ./start.sh          开发模式（前后端分别启动）
#   ./start.sh prod     生产模式（构建前端后启动后端）
#   ./start.sh stop     停止所有服务
#   ./start.sh build    仅构建

set -e

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOCAL_DIR="$PROJECT_DIR/local"
BIN_DIR="$LOCAL_DIR/bin"
LOG_DIR="$LOCAL_DIR/logs"
DATA_DIR="$LOCAL_DIR/data"
PID_DIR="$LOCAL_DIR/pids"

BACKEND_PORT=5678
FRONTEND_PORT=3012

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# 初始化本地目录
init_dirs() {
    mkdir -p "$BIN_DIR" "$LOG_DIR" "$DATA_DIR" "$PID_DIR" "$DATA_DIR/storage"
    log_info "本地目录已就绪: $LOCAL_DIR"
}

# 检查端口是否被占用，如果被占用则终止
check_port() {
    local port=$1
    local pid
    pid=$(lsof -ti :"$port" 2>/dev/null || true)
    if [ -n "$pid" ]; then
        log_warn "端口 $port 被占用 (PID: $pid)，正在终止..."
        kill -9 $pid 2>/dev/null || true
        sleep 1
    fi
}

# 停止所有服务
stop_all() {
    log_info "停止所有服务..."
    # 通过 PID 文件停止
    if [ -f "$PID_DIR/backend.pid" ]; then
        local pid=$(cat "$PID_DIR/backend.pid")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            log_info "后端已停止 (PID: $pid)"
        fi
        rm -f "$PID_DIR/backend.pid"
    fi
    if [ -f "$PID_DIR/frontend.pid" ]; then
        local pid=$(cat "$PID_DIR/frontend.pid")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            log_info "前端已停止 (PID: $pid)"
        fi
        rm -f "$PID_DIR/frontend.pid"
    fi
    # 兜底：直接按端口杀
    check_port $BACKEND_PORT
    check_port $FRONTEND_PORT
    log_info "所有服务已停止"
}

# 构建后端
build_backend() {
    log_info "构建后端..."
    cd "$PROJECT_DIR"
    CGO_ENABLED=0 go build -o "$BIN_DIR/huobao-drama" .
    chmod +x "$BIN_DIR/huobao-drama"
    log_info "后端构建完成: $BIN_DIR/huobao-drama"
}

# 构建前端
build_frontend() {
    log_info "构建前端..."
    cd "$PROJECT_DIR/web"
    npm run build
    log_info "前端构建完成: $PROJECT_DIR/web/dist/"
}

# 启动后端
start_backend() {
    cd "$PROJECT_DIR"

    # 确保配置文件存在
    if [ ! -f "configs/config.yaml" ]; then
        if [ -f "configs/config.example.yaml" ]; then
            cp configs/config.example.yaml configs/config.yaml
            log_warn "已从模板创建 configs/config.yaml，请根据需要修改配置"
        else
            log_error "找不到配置文件模板 configs/config.example.yaml"
            exit 1
        fi
    fi

    check_port $BACKEND_PORT

    if [ "$1" = "prod" ]; then
        # 生产模式：使用编译好的二进制
        if [ ! -f "$BIN_DIR/huobao-drama" ]; then
            build_backend
        fi
        nohup "$BIN_DIR/huobao-drama" > "$LOG_DIR/backend.log" 2>&1 &
    else
        # 开发模式：go run
        nohup go run main.go > "$LOG_DIR/backend.log" 2>&1 &
    fi

    echo $! > "$PID_DIR/backend.pid"
    log_info "后端已启动 (PID: $(cat "$PID_DIR/backend.pid"))"
    log_info "  API: http://localhost:$BACKEND_PORT/api/v1"
}

# 启动前端（开发模式）
start_frontend() {
    cd "$PROJECT_DIR/web"
    check_port $FRONTEND_PORT
    nohup npx vite --port $FRONTEND_PORT > "$LOG_DIR/frontend.log" 2>&1 &
    echo $! > "$PID_DIR/frontend.pid"
    log_info "前端已启动 (PID: $(cat "$PID_DIR/frontend.pid"))"
    log_info "  前端: http://localhost:$FRONTEND_PORT"
}

# 等待服务就绪
wait_for_backend() {
    local retries=15
    while [ $retries -gt 0 ]; do
        if curl -s "http://localhost:$BACKEND_PORT/health" > /dev/null 2>&1; then
            return 0
        fi
        retries=$((retries - 1))
        sleep 1
    done
    return 1
}

wait_for_frontend() {
    local retries=15
    while [ $retries -gt 0 ]; do
        if curl -s "http://localhost:$FRONTEND_PORT" > /dev/null 2>&1; then
            return 0
        fi
        retries=$((retries - 1))
        sleep 1
    done
    return 1
}

# 查看日志
show_logs() {
    echo ""
    echo -e "${BLUE}========== 实时日志 (Ctrl+C 退出) ==========${NC}"
    echo -e "  后端日志: tail -f $LOG_DIR/backend.log"
    echo -e "  前端日志: tail -f $LOG_DIR/frontend.log"
    echo ""
    tail -f "$LOG_DIR/backend.log" "$LOG_DIR/frontend.log" 2>/dev/null || tail -f "$LOG_DIR/backend.log"
}

# 查看状态
show_status() {
    echo ""
    echo -e "${BLUE}========== 服务状态 ==========${NC}"

    # 后端
    if curl -s "http://localhost:$BACKEND_PORT/health" > /dev/null 2>&1; then
        echo -e "  后端: ${GREEN}运行中${NC} http://localhost:$BACKEND_PORT"
    else
        echo -e "  后端: ${RED}未运行${NC}"
    fi

    # 前端
    if curl -s "http://localhost:$FRONTEND_PORT" > /dev/null 2>&1; then
        echo -e "  前端: ${GREEN}运行中${NC} http://localhost:$FRONTEND_PORT"
    else
        echo -e "  前端: ${RED}未运行${NC}"
    fi

    echo ""
    echo -e "  本地目录: $LOCAL_DIR"
    echo -e "  后端日志: $LOG_DIR/backend.log"
    echo -e "  前端日志: $LOG_DIR/frontend.log"
    echo ""
}

# ===================== 主逻辑 =====================

init_dirs

case "${1:-dev}" in
    dev)
        log_info "启动开发模式..."
        start_backend
        if wait_for_backend; then
            log_info "后端就绪"
        else
            log_error "后端启动失败，查看日志: $LOG_DIR/backend.log"
            exit 1
        fi
        start_frontend
        if wait_for_frontend; then
            log_info "前端就绪"
        else
            log_warn "前端启动较慢，请稍候访问 http://localhost:$FRONTEND_PORT"
        fi
        show_status
        show_logs
        ;;

    prod)
        log_info "启动生产模式..."
        build_frontend
        build_backend
        start_backend prod
        if wait_for_backend; then
            log_info "服务就绪"
        else
            log_error "后端启动失败，查看日志: $LOG_DIR/backend.log"
            exit 1
        fi
        show_status
        echo -e "\n访问: ${GREEN}http://localhost:$BACKEND_PORT${NC}\n"
        show_logs
        ;;

    stop)
        stop_all
        ;;

    restart)
        stop_all
        sleep 1
        exec "$0" dev
        ;;

    build)
        build_backend
        build_frontend
        log_info "构建完成"
        ;;

    status)
        show_status
        ;;

    logs)
        show_logs
        ;;

    *)
        echo "火宝短剧 管理脚本"
        echo ""
        echo "用法: $0 {dev|prod|stop|restart|build|status|logs}"
        echo ""
        echo "  dev      开发模式（前后端分别启动，热重载）"
        echo "  prod     生产模式（构建后启动，前后端合一）"
        echo "  stop     停止所有服务"
        echo "  restart  重启服务（开发模式）"
        echo "  build    仅构建前后端"
        echo "  status   查看服务状态"
        echo "  logs     查看实时日志"
        ;;
esac
