FROM oven/bun:1-alpine

RUN bun add -g @shellhaki/envi-cli
ENV PATH="/root/.bun/bin:${PATH}"

WORKDIR /app
COPY . .
RUN bun install --frozen-lockfile

ENTRYPOINT ["envi", "run", "--"]
EXPOSE 3000
CMD ["bun", "index.ts"]
