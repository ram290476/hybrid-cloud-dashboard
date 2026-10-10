FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY frontend/ /usr/share/nginx/html/
COPY deploy/base/frontend/default.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
