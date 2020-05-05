FROM novapokemon/nova-server-base:latest

ENV executable="executable"
COPY $executable .

CMD ["sh", "-c", "./$executable"]