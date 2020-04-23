FROM golang:latest

ENV executable="executable"

RUN mkdir /service
WORKDIR /service
COPY $executable .

COPY dockerize .
RUN chmod +x dockerize

CMD ["$executable"]