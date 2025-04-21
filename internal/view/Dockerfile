FROM node:23-slim-alphine3.17 as base
COPY package*.json ./
RUN npm install --no-cache-dir
EXPOSE 3000


FROM base as builder
WORKDIR /app
COPY . .
CMD [ "npm", "run", "build" ]


FROM base as production
WORKDIR /app
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package*.json ./
COPY --from=builder /app/public ./public
COPY --from=builder /app/.env ./.env
COPY --from=builder /app/package-lock.json ./package-lock.json
CMD [ "npm", "run", "start" ]