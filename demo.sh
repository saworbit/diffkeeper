#!/bin/bash
set -e

echo "🚀 DiffKeeper Demo: State Survives Container Nuke"
echo "=================================================="
echo ""

CONTAINER_NAME="diffkeeper-postgres-demo"
IMAGE_NAME="diffkeeper-postgres:latest"

# Cleanup any existing container
echo "🧹 Cleaning up existing containers..."
docker rm -f $CONTAINER_NAME 2>/dev/null || true

# Build the image
echo ""
echo "🔨 Building DiffKeeper + Postgres image..."
docker build -f Dockerfile.postgres -t $IMAGE_NAME .

# Start container with volume for deltas
echo ""
echo "📦 Starting Postgres with DiffKeeper..."
docker run -d \
  --name $CONTAINER_NAME \
  -v diffkeeper-deltas:/deltas \
  -e POSTGRES_PASSWORD=password \
  $IMAGE_NAME

# Wait for Postgres to be ready
echo ""
echo "⏳ Waiting for Postgres to initialize..."
sleep 10

# Create test data
echo ""
echo "📝 Creating test data..."
docker exec $CONTAINER_NAME psql -U postgres -d testdb -c "
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100),
    email VARCHAR(100),
    created_at TIMESTAMP DEFAULT NOW()
);

INSERT INTO users (name, email) VALUES 
    ('Alice Johnson', 'alice@example.com'),
    ('Bob Smith', 'bob@example.com'),
    ('Charlie Davis', 'charlie@example.com');
"

# Verify data exists
echo ""
echo "✅ Verifying data before nuke..."
docker exec $CONTAINER_NAME psql -U postgres -d testdb -c "SELECT COUNT(*) as total_users FROM users;"

# Show delta storage size
echo ""
echo "💾 Delta storage size:"
docker exec $CONTAINER_NAME du -sh /deltas

# THE CRITICAL TEST: Kill and restart
echo ""
echo "💥 NUKING CONTAINER (killing without graceful shutdown)..."
docker kill $CONTAINER_NAME

echo ""
echo "⏳ Waiting 2 seconds..."
sleep 2

echo ""
echo "🔄 Restarting container..."
docker start $CONTAINER_NAME

echo ""
echo "⏳ Waiting for Postgres to recover..."
sleep 8

# Verify data survived
echo ""
echo "🎯 THE MOMENT OF TRUTH: Checking if data survived..."
echo ""

RESULT=$(docker exec $CONTAINER_NAME psql -U postgres -d testdb -t -c "SELECT COUNT(*) FROM users;")
COUNT=$(echo $RESULT | xargs)

if [ "$COUNT" == "3" ]; then
    echo "✅✅✅ SUCCESS! Data survived the nuke! ✅✅✅"
    echo ""
    echo "📊 Recovered data:"
    docker exec $CONTAINER_NAME psql -U postgres -d testdb -c "SELECT * FROM users;"
    echo ""
    echo "🎉 DiffKeeper successfully restored state from deltas!"
else
    echo "❌ FAILED: Expected 3 users, found $COUNT"
    exit 1
fi

# Show logs
echo ""
echo "📋 DiffKeeper logs:"
docker logs $CONTAINER_NAME 2>&1 | grep -E "(RedShift|BlueShift|Watching)" | tail -20

echo ""
echo "🏁 Demo complete! Container logs available with: docker logs $CONTAINER_NAME"
echo "🧹 Cleanup with: docker rm -f $CONTAINER_NAME && docker volume rm diffkeeper-deltas"