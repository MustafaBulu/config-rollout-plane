FROM maven:3.9.11-eclipse-temurin-21 AS build

WORKDIR /src
COPY pom.xml ./
RUN mvn -q -DskipTests dependency:go-offline

COPY src ./src
RUN mvn -q -DskipTests package

FROM eclipse-temurin:21-jre

RUN useradd --system --create-home --uid 10001 app
USER app

WORKDIR /app
COPY --from=build /src/target/demo-service-0.1.0-SNAPSHOT.jar /app/demo-service.jar

EXPOSE 8090
ENTRYPOINT ["java", "-jar", "/app/demo-service.jar"]
