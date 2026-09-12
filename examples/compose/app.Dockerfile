# syntax=docker/dockerfile:1

# The Spring Boot example, packaged the way a service is packaged in
# production, with one addition: the faultline binary, used at start-up to
# build a trust store the JVM will accept. Nothing else about the application
# changes, and no Faultline code is compiled into it.

FROM maven:3-eclipse-temurin-21 AS app
WORKDIR /src
COPY examples/spring-boot/pom.xml ./
RUN mvn -B -q dependency:go-offline
COPY examples/spring-boot/src ./src
RUN mvn -B -q package -DskipTests

# The helper only has to run `faultline trust java`, so it is built without the
# ui tag: no UI is embedded and nothing serves one.
FROM golang:1.25-alpine AS faultline
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /out/faultline ./cmd/faultline

# A JDK rather than a JRE: the trust store is built by copying the JDK's own
# cacerts and adding the Faultline CA with the JDK's own keytool.
FROM eclipse-temurin:21-jdk
COPY --from=faultline /out/faultline /usr/local/bin/faultline
COPY --from=app /src/target/*.jar /app/app.jar
EXPOSE 8080

# The entrypoint. `faultline trust java` builds the trust store from the
# mounted CA, renders JAVA_TOOL_OPTIONS with it and with the proxy variables
# already in the environment, and executes the command after -- with that
# variable set. The application is started the way it always was.
ENTRYPOINT ["faultline", "trust", "java", "--", "java", "-jar", "/app/app.jar"]
