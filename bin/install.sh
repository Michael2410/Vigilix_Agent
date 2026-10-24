#!/usr/bin/env bash
set -e

# ==============================================================================
# Vigilix Agent - Instalador y Auto-Servicio Systemd para Linux
# Uso: sudo ./install.sh --token "<TOKEN>" [--server "http://<IP>/api"]
# ==============================================================================

INSTALL_DIR="/opt/vigilix-agent"
SERVICE_NAME="vigilix-agent"

SERVER_URL=""
TOKEN=""

# Parsear argumentos
while [[ $# -gt 0 ]]; do
  case $1 in
    --server)
      SERVER_URL="$2"
      shift 2
      ;;
    --token)
      TOKEN="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [ "$EUID" -ne 0 ]; then
  echo "❌ Error: Este instalador debe ejecutarse con privilegios de root (usa sudo)."
  echo "Uso: sudo ./install.sh --token \"<TOKEN>\" [--server \"http://<IP>/api\"]"
  exit 1
fi

if [ -z "$TOKEN" ]; then
  echo ""
  echo "🔑 Configuración de Enrolamiento de Vigilix"
  echo "Copia el token generado en la web (Host -> Enrolar Agente)."
  read -r -p "👉 Ingresa el TOKEN: " TOKEN
  if [ -z "$TOKEN" ]; then
    echo "❌ Error: El token no puede estar vacío."
    exit 1
  fi
fi

if [ -z "$SERVER_URL" ]; then
  DEFAULT_URL="http://192.168.1.3/api"
  read -r -p "🌐 URL del servidor Vigilix [Enter para usar '$DEFAULT_URL']: " INPUT_URL
  if [ -n "$INPUT_URL" ]; then
    SERVER_URL="$INPUT_URL"
  else
    SERVER_URL="$DEFAULT_URL"
  fi
fi

echo "=========================================="
echo "🚀 Iniciando Instalación de Vigilix Agent"
echo "=========================================="
echo "• Directorio destino: $INSTALL_DIR"
echo "• Servidor API:      $SERVER_URL"
echo ""

# 1. Crear directorio
mkdir -p "$INSTALL_DIR"

# 2. Copiar binario local
if [ -f "./vigilix-agent-linux" ]; then
  echo "📦 Copiando binario vigilix-agent-linux a $INSTALL_DIR/vigilix-agent..."
  cp "./vigilix-agent-linux" "$INSTALL_DIR/vigilix-agent"
elif [ -f "./vigilix-agent" ]; then
  echo "📦 Copiando binario vigilix-agent a $INSTALL_DIR/vigilix-agent..."
  cp "./vigilix-agent" "$INSTALL_DIR/vigilix-agent"
elif [ -f "$INSTALL_DIR/vigilix-agent" ]; then
  echo "ℹ️ Binario ya presente en $INSTALL_DIR/vigilix-agent"
else
  echo "❌ Error: No se encontró el archivo 'vigilix-agent-linux' en la carpeta actual."
  echo "Asegúrate de copiar 'vigilix-agent-linux' e 'install.sh' en la misma carpeta."
  exit 1
fi

chmod +x "$INSTALL_DIR/vigilix-agent"

# 3. Enrolamiento
echo "🔑 Enrolando agente contra el servidor Vigilix..."
"$INSTALL_DIR/vigilix-agent" -enroll -server "$SERVER_URL" -token "$TOKEN" -config "$INSTALL_DIR/config.yaml"

if [ ! -f "$INSTALL_DIR/config.yaml" ]; then
  echo "❌ Error: No se pudo generar $INSTALL_DIR/config.yaml. Revisa el token o la red."
  exit 1
fi

# 4. Crear unidad systemd
echo "⚙️ Creando servicio del sistema /etc/systemd/system/${SERVICE_NAME}.service..."
cat << EOF > /etc/systemd/system/${SERVICE_NAME}.service
[Unit]
Description=Vigilix Monitoring Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/vigilix-agent -config $INSTALL_DIR/config.yaml
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

# 5. Activar y arrancar servicio
echo "🔄 Recargando systemd y arrancando servicio..."
systemctl daemon-reload
systemctl enable --now ${SERVICE_NAME}

echo ""
echo "=========================================="
echo "✅ ¡Vigilix Agent instalado con éxito!"
echo "=========================================="
echo "El agente ya está corriendo en segundo plano como servicio del sistema."
echo "Comandos útiles:"
echo " • Ver estado: sudo systemctl status ${SERVICE_NAME}"
echo " • Ver logs:   sudo journalctl -u ${SERVICE_NAME} -f"
echo " • Reiniciar:  sudo systemctl restart ${SERVICE_NAME}"
echo ""

systemctl status ${SERVICE_NAME} --no-pager
