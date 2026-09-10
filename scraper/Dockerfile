# Build from the repository root: the scraper reads schema/schema.sql.
FROM python:3.13-slim
WORKDIR /app

COPY scraper/pyproject.toml scraper/pyproject.toml
COPY scraper/shanyrak_scraper scraper/shanyrak_scraper
RUN pip install --no-cache-dir ./scraper

COPY schema schema

ENV PYTHONUNBUFFERED=1
ENTRYPOINT ["python", "-m", "shanyrak_scraper"]
CMD ["scan", "--every", "6h"]
