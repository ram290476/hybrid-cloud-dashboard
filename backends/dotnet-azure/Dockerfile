FROM mcr.microsoft.com/dotnet/sdk:10.0 AS build
WORKDIR /src
COPY backends/dotnet-azure /src/backends/dotnet-azure
COPY contract/fixtures /src/contract/fixtures
WORKDIR /src/backends/dotnet-azure
RUN dotnet publish -c Release -o /out

FROM mcr.microsoft.com/dotnet/aspnet:10.0
WORKDIR /app
COPY --from=build /out /app
COPY contract/fixtures /fixtures
ENV FIXTURES_DIR=/fixtures ASPNETCORE_URLS=http://+:8080 PORT=8080
EXPOSE 8080
USER $APP_UID
ENTRYPOINT ["dotnet", "DashboardApi.dll"]
