# syntax=docker/dockerfile:1.7
# gogi's database migrations: brings the database of DATABASE_URL up to date, before the services start.
#
#   docker run --rm -e DATABASE_URL=postgres://gogi:...@db:5432/gogi?sslmode=require gogi-migrations
#
# The repository's .dockerignore leaves the migrations out of the services' images; this image has its own,
# docker/migrations.Dockerfile.dockerignore
FROM migrate/migrate:v4.18.3

COPY migrations /migrations

ENTRYPOINT ["/bin/sh", "-c", "exec migrate -path /migrations -database \"$DATABASE_URL\" \"${@:-up}\"", "--"]
