# Runtime image for Paper servers. Keep it minimal: the panel mounts the server
# directory at /data and runs `java -jar paper.jar --nogui`.
FROM docker.io/library/eclipse-temurin:21-jre

LABEL org.opencontainers.image.title="DelphiMC Java 21 runtime"
LABEL org.opencontainers.image.description="Temurin 21 JRE for Paper servers"

WORKDIR /data
