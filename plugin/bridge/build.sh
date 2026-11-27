#!/bin/sh
# Builds the bridge plugin jar and installs it where the agent embeds it.
# Pinned against Paper 1.21.11; bump the three versions together.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
CACHE=${BRIDGE_CACHE:-/tmp/delphi-bridge-deps}
mkdir -p "$CACHE"
PAPER=1.21.11-R0.1-SNAPSHOT
HOST=https://repo.papermc.io/repository/maven-public/io/papermc/paper/paper-api/$PAPER
ADV=5.2.0
fetch() { [ -f "$CACHE/$2" ] || curl -fsS -o "$CACHE/$2" "$1"; }
fetch "$HOST/maven-metadata.xml" metadata.xml
SNAP=$(sed -n 's#.*<value>\([^<]*\)</value>.*#\1#p' "$CACHE/metadata.xml" | head -1)
fetch "$HOST/paper-api-$SNAP.jar" paper-api.jar
fetch "https://repo1.maven.org/maven2/com/google/code/gson/gson/2.11.0/gson-2.11.0.jar" gson.jar
fetch "https://repo1.maven.org/maven2/net/kyori/adventure-api/$ADV/adventure-api-$ADV.jar" "adventure-api-$ADV.jar"
fetch "https://repo1.maven.org/maven2/net/kyori/adventure-key/$ADV/adventure-key-$ADV.jar" "adventure-key-$ADV.jar"
fetch "https://repo1.maven.org/maven2/net/kyori/adventure-text-serializer-plain/$ADV/adventure-text-serializer-plain-$ADV.jar" "adventure-text-serializer-plain-$ADV.jar"
fetch "https://repo1.maven.org/maven2/net/kyori/examination-api/1.3.0/examination-api-1.3.0.jar" examination-api-1.3.0.jar
CP="$CACHE/paper-api.jar:$CACHE/gson.jar:$CACHE/adventure-api-$ADV.jar:$CACHE/adventure-key-$ADV.jar:$CACHE/adventure-text-serializer-plain-$ADV.jar:$CACHE/examination-api-1.3.0.jar"
rm -rf "$HERE/build"
mkdir -p "$HERE/build"
javac --release 21 -cp "$CP" -d "$HERE/build" "$HERE"/src/dev/delphi/bridge/*.java
cp "$HERE/plugin.yml" "$HERE/build/"
(cd "$HERE/build" && jar cf ../DelphiBridge.jar .)
mkdir -p "$HERE/../../internal/agent/bridge"
cp "$HERE/DelphiBridge.jar" "$HERE/../../internal/agent/bridge/DelphiBridge.jar"
echo "built $HERE/DelphiBridge.jar"
