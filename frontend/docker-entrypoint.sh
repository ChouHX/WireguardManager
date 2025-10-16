#!/bin/sh

# Start Next.js server in the background
echo "Starting Next.js server..."
cd /app

# Set hostname to listen on all interfaces
export HOSTNAME=0.0.0.0
export PORT=3000

node server.js &

# Wait for Next.js to be ready
echo "Waiting for Next.js to start..."
sleep 5

# Check if Next.js is running
if ! nc -z 127.0.0.1 3000; then
    echo "Warning: Next.js may not be ready yet"
fi

# Start Nginx in the foreground
echo "Starting Nginx..."
nginx -g 'daemon off;'
