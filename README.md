# 🛰️ Vigilix Agent & Collector

Agente ligero y colector central multiplataforma (Linux / Windows) para el sistema de observabilidad y monitoreo de infraestructura **Vigilix**.

---

## 1. Despliegue con Docker Compose (Servidor Central)

El servidor central corre el backend NestJS, frontend Next.js, TimescaleDB, Redis, Nginx y el **Colector Go** en contenedores Docker.

Desde la carpeta raíz del proyecto (`Vigilix`):

```bash
# Levantar todos los servicios en segundo plano y compilar cambios
docker compose up -d --build

# Ver el estado de todos los contenedores
docker compose ps

# Ver los logs del colector central
docker compose logs -f collector

# Reiniciar servicios si hiciste cambios
docker compose restart
```

---

## 2. Instalación de Agentes en Servidores o PCs

Los binarios precompilados y los instaladores automatizados se encuentran en la carpeta `Vigilix_Agent/bin/`:

- `vigilix-agent-linux` + `install.sh` (Para Ubuntu / Debian / CentOS / RHEL)
- `vigilix-agent-windows.exe` + `install.ps1` (Para Windows 10, 11 y Windows Server)

---

### 🐧 A. Instalación en Linux (Ubuntu / Debian / CentOS)

#### Pasos:

1. Copia la carpeta o los archivos `install.sh` y `vigilix-agent-linux` a la máquina destino (por ejemplo en `/opt/files/bin/` o `/tmp/`).
2. Abre una terminal y navega hasta donde copiaste los archivos:
   ```bash
   cd /opt/files/bin
   ```
3. Ejecuta el instalador con privilegios de superusuario:
   ```bash
   sudo ./install.sh
   ```
4. El instalador te pedirá interactivamente:
   - **Token:** El token de un solo uso generado en la plataforma web (*Host -> Enrolar Agente*).
   - **URL del Servidor:** La URL del backend (ejemplo: `http://192.168.1.3/api`).

El script copiará el binario a `/opt/vigilix-agent/`, creará y activará automáticamente el servicio `systemd` (`vigilix-agent.service`).

---

#### ⚠️ Solución de Problemas en Linux:
Si al ejecutar `sudo ./install.sh` recibes un error como `sudo: ./install.sh: command not found` o `syntax error near unexpected token`, se debe a los saltos de línea de Windows (`CRLF`) o falta de permisos. Ejecuta lo siguiente:

```bash
# 1. Quitar los saltos de línea de Windows (\r)
sed -i 's/\r$//' install.sh

# 2. Dar permisos de ejecución tanto al script como al binario
chmod +x install.sh vigilix-agent-linux

# 3. Ejecutar el instalador
sudo ./install.sh
```

#### Comandos útiles del servicio en Linux:
```bash
# Ver estado del agente
sudo systemctl status vigilix-agent

# Ver logs en vivo
sudo journalctl -u vigilix-agent -f

# Reiniciar agente
sudo systemctl restart vigilix-agent
```

---

### 🪟 B. Instalación en Windows (Windows 10, 11, Windows Server)

#### Método 1: Con el instalador automático (`install.ps1`)

1. Copia `vigilix-agent-windows.exe` e `install.ps1` a la máquina de destino.
2. Abre **PowerShell como Administrador** en esa carpeta.
3. Ejecuta el script:
   ```powershell
   .\install.ps1
   ```
4. Ingresa el **TOKEN** cuando te lo solicite y la **URL del Servidor** (por defecto `http://localhost/api` o la IP de tu servidor central `http://192.168.1.3/api`).

El instalador:
- Copiará el ejecutable a `C:\Program Files\VigilixAgent\`.
- Realizará el enrolamiento generando `config.yaml`.
- Registrará e iniciará una **Tarea Programada de Sistema** que corre en segundo plano y se reinicia automáticamente con Windows.