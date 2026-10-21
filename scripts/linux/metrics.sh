#!/bin/sh
# Script de recolección de métricas para Linux (§8.5)
# Imprime secciones delimitadas legibles por el parser de Go

echo "### stat"
grep '^cpu ' /proc/stat 2>/dev/null

echo "### meminfo"
cat /proc/meminfo 2>/dev/null

echo "### loadavg"
cat /proc/loadavg 2>/dev/null

echo "### uptime"
cat /proc/uptime 2>/dev/null

echo "### netdev"
cat /proc/net/dev 2>/dev/null

echo "### df"
df -P -B1 -x tmpfs -x devtmpfs -x overlay -x squashfs 2>/dev/null

echo "### os"
cat /etc/os-release 2>/dev/null

echo "### cpuinfo"
grep -m1 'model name' /proc/cpuinfo 2>/dev/null
echo "cores: $(grep -c '^processor' /proc/cpuinfo 2>/dev/null)"

echo "### services"
for s in "$@"; do
  if [ -n "$s" ]; then
    st=$(systemctl is-active "$s" 2>/dev/null || echo "unknown")
    echo "$s $st"
  fi
done
