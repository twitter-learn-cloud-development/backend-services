import glob

# Fix go files
go_files = glob.glob('cmd/*/main.go')
for f in go_files:
    with open(f, 'r', encoding='utf-8') as file:
        content = file.read()
    content = content.replace('"http://localhost:14268/api/traces"', '"localhost:4317"')
    with open(f, 'w', encoding='utf-8') as file:
        file.write(content)
    print(f'Fixed {f}')

# Fix docker-compose files
for f in ['docker-compose.yaml', 'docker-compose-learn.yaml']:
    with open(f, 'r', encoding='utf-8') as file:
        content = file.read()
    content = content.replace('http://jaeger:14268/api/traces', 'jaeger:4317')
    content = content.replace('      - "6831:6831/udp"', '      - "6831:6831/udp"\n      - "4317:4317"')
    with open(f, 'w', encoding='utf-8') as file:
        file.write(content)
    print(f'Fixed {f}')
