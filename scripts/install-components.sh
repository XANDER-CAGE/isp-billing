#!/bin/bash
# Installation script for Netspire-Go IP Pool and Disconnect components
# Equivalent to mod_ippool.erl and mod_disconnect_*.erl installation

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
NETSPIRE_GO_DIR="/opt/netspire-go"
FREERADIUS_CONFIG_DIR="/etc/freeradius/3.0"
REDIS_CONFIG_DIR="/etc/redis"

echo -e "${BLUE}🚀 Installing Netspire-Go IP Pool and Disconnect Components${NC}"
echo "============================================================"

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}❌ This script must be run as root${NC}"
   exit 1
fi

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to install FreeRADIUS if not present
install_freeradius() {
    if ! command_exists radiusd; then
        echo -e "${YELLOW}📦 Installing FreeRADIUS...${NC}"
        
        if command_exists apt-get; then
            # Debian/Ubuntu
            apt-get update
            apt-get install -y freeradius freeradius-utils freeradius-rest
        elif command_exists yum; then
            # CentOS/RHEL
            yum install -y freeradius freeradius-utils freeradius-rest
        elif command_exists dnf; then
            # Fedora
            dnf install -y freeradius freeradius-utils freeradius-rest
        else
            echo -e "${RED}❌ Unsupported package manager. Please install FreeRADIUS manually${NC}"
            exit 1
        fi
        
        echo -e "${GREEN}✅ FreeRADIUS installed successfully${NC}"
    else
        echo -e "${GREEN}✅ FreeRADIUS already installed${NC}"
    fi
}

# Function to install Redis if not present
install_redis() {
    if ! command_exists redis-server; then
        echo -e "${YELLOW}📦 Installing Redis...${NC}"
        
        if command_exists apt-get; then
            # Debian/Ubuntu
            apt-get install -y redis-server
        elif command_exists yum; then
            # CentOS/RHEL
            yum install -y redis
        elif command_exists dnf; then
            # Fedora
            dnf install -y redis
        else
            echo -e "${RED}❌ Unsupported package manager. Please install Redis manually${NC}"
            exit 1
        fi
        
        echo -e "${GREEN}✅ Redis installed successfully${NC}"
    else
        echo -e "${GREEN}✅ Redis already installed${NC}"
    fi
}

# Function to configure FreeRADIUS
configure_freeradius() {
    echo -e "${YELLOW}🔧 Configuring FreeRADIUS for Netspire-Go...${NC}"
    
    # Backup original configuration
    if [ ! -d "${FREERADIUS_CONFIG_DIR}/backup" ]; then
        mkdir -p "${FREERADIUS_CONFIG_DIR}/backup"
        cp -r "${FREERADIUS_CONFIG_DIR}/mods-enabled" "${FREERADIUS_CONFIG_DIR}/backup/"
        cp -r "${FREERADIUS_CONFIG_DIR}/sites-enabled" "${FREERADIUS_CONFIG_DIR}/backup/"
        echo -e "${BLUE}📋 Original FreeRADIUS configuration backed up${NC}"
    fi
    
    # Copy Netspire REST module
    cp freeradius/mods-available/netspire-rest "${FREERADIUS_CONFIG_DIR}/mods-available/"
    cp freeradius/mods-available/netspire-ippool "${FREERADIUS_CONFIG_DIR}/mods-available/"
    
    # Enable modules
    ln -sf "${FREERADIUS_CONFIG_DIR}/mods-available/netspire-rest" "${FREERADIUS_CONFIG_DIR}/mods-enabled/"
    ln -sf "${FREERADIUS_CONFIG_DIR}/mods-available/netspire-ippool" "${FREERADIUS_CONFIG_DIR}/mods-enabled/"
    
    # Copy site configuration
    cp freeradius/sites-available/netspire-with-ippool "${FREERADIUS_CONFIG_DIR}/sites-available/"
    
    # Enable site
    ln -sf "${FREERADIUS_CONFIG_DIR}/sites-available/netspire-with-ippool" "${FREERADIUS_CONFIG_DIR}/sites-enabled/"
    
    # Update clients.conf with test configuration
    if ! grep -q "# Netspire-Go clients" "${FREERADIUS_CONFIG_DIR}/clients.conf"; then
        cat >> "${FREERADIUS_CONFIG_DIR}/clients.conf" << 'EOF'

# Netspire-Go clients
client localhost {
    ipaddr = 127.0.0.1
    secret = testing123
    shortname = localhost
    nastype = other
}

client testnas {
    ipaddr = 192.168.1.0/24
    secret = testing123
    shortname = testnas
    nastype = cisco
}
EOF
        echo -e "${BLUE}📝 Added test clients to FreeRADIUS configuration${NC}"
    fi
    
    echo -e "${GREEN}✅ FreeRADIUS configured for Netspire-Go${NC}"
}

# Function to configure Redis
configure_redis() {
    echo -e "${YELLOW}🔧 Configuring Redis for Netspire-Go...${NC}"
    
    # Backup original Redis configuration
    if [ -f "/etc/redis/redis.conf" ] && [ ! -f "/etc/redis/redis.conf.backup" ]; then
        cp /etc/redis/redis.conf /etc/redis/redis.conf.backup
        echo -e "${BLUE}📋 Original Redis configuration backed up${NC}"
    fi
    
    # Basic Redis configuration for Netspire-Go
    cat > /etc/redis/netspire.conf << 'EOF'
# Redis configuration for Netspire-Go
port 6379
bind 127.0.0.1
timeout 0
keepalive 0
databases 16
save 900 1
save 300 10
save 60 10000
dbfilename netspire.rdb
dir /var/lib/redis/
maxmemory-policy allkeys-lru
EOF
    
    echo -e "${GREEN}✅ Redis configured for Netspire-Go${NC}"
}

# Function to create systemd service files
create_systemd_services() {
    echo -e "${YELLOW}🔧 Creating systemd service files...${NC}"
    
    # Netspire-Go service
    cat > /etc/systemd/system/netspire-go.service << EOF
[Unit]
Description=Netspire-Go Billing Service
After=network.target redis.service postgresql.service
Wants=redis.service postgresql.service

[Service]
Type=simple
User=netspire
Group=netspire
ExecStart=${NETSPIRE_GO_DIR}/netspire-go --config=${NETSPIRE_GO_DIR}/config.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

# Environment variables
Environment=NETSPIRE_ENV=production
Environment=NETSPIRE_LOG_LEVEL=info

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${NETSPIRE_GO_DIR}/logs

[Install]
WantedBy=multi-user.target
EOF

    # Create netspire user if doesn't exist
    if ! id "netspire" &>/dev/null; then
        useradd -r -s /bin/false -d ${NETSPIRE_GO_DIR} netspire
        echo -e "${BLUE}👤 Created netspire user${NC}"
    fi
    
    systemctl daemon-reload
    echo -e "${GREEN}✅ Systemd services created${NC}"
}

# Function to test installation
test_installation() {
    echo -e "${YELLOW}🧪 Testing installation...${NC}"
    
    # Test Redis connectivity
    if redis-cli ping | grep -q "PONG"; then
        echo -e "${GREEN}✅ Redis is running and accessible${NC}"
    else
        echo -e "${RED}❌ Redis connection failed${NC}"
        return 1
    fi
    
    # Test FreeRADIUS configuration
    if radiusd -C; then
        echo -e "${GREEN}✅ FreeRADIUS configuration is valid${NC}"
    else
        echo -e "${RED}❌ FreeRADIUS configuration has errors${NC}"
        return 1
    fi
    
    # Test Netspire-Go binary
    if [ -f "./netspire-go" ]; then
        if ./netspire-go --version; then
            echo -e "${GREEN}✅ Netspire-Go binary is working${NC}"
        else
            echo -e "${RED}❌ Netspire-Go binary test failed${NC}"
            return 1
        fi
    else
        echo -e "${YELLOW}⚠️ Netspire-Go binary not found, build it first${NC}"
    fi
    
    echo -e "${GREEN}✅ All tests passed${NC}"
}

# Function to show service status
show_status() {
    echo -e "${BLUE}📊 Service Status:${NC}"
    echo "=================="
    
    # Redis status
    if systemctl is-active --quiet redis; then
        echo -e "Redis: ${GREEN}Running${NC}"
    else
        echo -e "Redis: ${RED}Stopped${NC}"
    fi
    
    # FreeRADIUS status
    if systemctl is-active --quiet freeradius; then
        echo -e "FreeRADIUS: ${GREEN}Running${NC}"
    else
        echo -e "FreeRADIUS: ${RED}Stopped${NC}"
    fi
    
    # Netspire-Go status
    if systemctl is-active --quiet netspire-go; then
        echo -e "Netspire-Go: ${GREEN}Running${NC}"
    else
        echo -e "Netspire-Go: ${RED}Stopped${NC}"
    fi
}

# Function to start services
start_services() {
    echo -e "${YELLOW}🚀 Starting services...${NC}"
    
    # Start and enable Redis
    systemctl start redis
    systemctl enable redis
    
    # Start and enable FreeRADIUS
    systemctl start freeradius
    systemctl enable freeradius
    
    # Note: Netspire-Go should be started manually after configuration
    echo -e "${BLUE}ℹ️ Start Netspire-Go manually after configuring database connection${NC}"
    echo -e "${BLUE}   sudo systemctl start netspire-go${NC}"
    
    echo -e "${GREEN}✅ Services started${NC}"
}

# Main installation flow
main() {
    echo -e "${BLUE}Starting installation process...${NC}"
    
    install_freeradius
    install_redis
    configure_freeradius
    configure_redis
    create_systemd_services
    test_installation
    start_services
    show_status
    
    echo ""
    echo -e "${GREEN}🎉 Installation completed successfully!${NC}"
    echo ""
    echo -e "${BLUE}Next steps:${NC}"
    echo "1. Configure database connection in config.yaml"
    echo "2. Build Netspire-Go: make build"
    echo "3. Start Netspire-Go: sudo systemctl start netspire-go"
    echo "4. Test with: radtest testuser testpass localhost:1812 0 testing123"
    echo ""
    echo -e "${BLUE}Configuration files:${NC}"
    echo "- FreeRADIUS: ${FREERADIUS_CONFIG_DIR}"
    echo "- Redis: /etc/redis/netspire.conf"
    echo "- Netspire-Go: ${NETSPIRE_GO_DIR}/config.yaml"
    echo ""
    echo -e "${BLUE}Logs:${NC}"
    echo "- FreeRADIUS: journalctl -u freeradius"
    echo "- Redis: journalctl -u redis"
    echo "- Netspire-Go: journalctl -u netspire-go"
}

# Run main function
main "$@" 