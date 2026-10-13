<?php
// Launcher Brasil — receptor de uploads da base.
// Gerado pelo painel do administrador. Envie este arquivo para uma pasta do
// seu site (ex.: public_html/ac/upload.php). Os pacotes ficam em ./files/.
// A chave não está neste arquivo, só o hash dela; não edite a linha abaixo.
const KEY_SHA256 = '{{KEY_SHA256}}';
const MAX_FILE_BYTES = 21474836480; // 20 GB

header('Content-Type: application/json; charset=utf-8');
header('Cache-Control: no-store');
header('X-Content-Type-Options: nosniff');

function out($code, $data) {
    http_response_code($code);
    echo json_encode($data);
    exit;
}

$key = isset($_SERVER['HTTP_X_KEY']) ? (string)$_SERVER['HTTP_X_KEY'] : '';
if ($key === '' || !hash_equals(KEY_SHA256, hash('sha256', $key))) {
    out(403, ['error' => 'chave inválida']);
}

$dir = __DIR__ . DIRECTORY_SEPARATOR . 'files';
if (!is_dir($dir) && !@mkdir($dir, 0755, true)) {
    out(500, ['error' => 'não foi possível criar a pasta files ao lado do upload.php']);
}
$ht = $dir . DIRECTORY_SEPARATOR . '.htaccess';
if (!is_file($ht)) {
    @file_put_contents($ht, "Options -Indexes\n<FilesMatch \"\\.(php[0-9]?|phtml|phar|pht|cgi|pl|py|sh)(\\.|$)\">\n  Require all denied\n</FilesMatch>\n<IfModule mod_php.c>\n  php_flag engine off\n</IfModule>\n");
}

$action = isset($_GET['action']) ? (string)$_GET['action'] : '';

if ($action === 'ping') {
    $free = @disk_free_space($dir);
    out(200, ['ok' => true, 'php' => PHP_VERSION, 'free' => $free === false ? null : $free]);
}

if ($action === 'list') {
    $files = [];
    foreach (scandir($dir) as $f) {
        if ($f === '' || $f[0] === '.' || substr($f, -5) === '.part') {
            continue;
        }
        $files[] = ['name' => $f, 'size' => filesize($dir . DIRECTORY_SEPARATOR . $f)];
    }
    out(200, ['files' => $files]);
}

$name = isset($_GET['file']) ? (string)$_GET['file'] : '';
if (!preg_match('/^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,150}\.(zip|json|jpg|jpeg|png|webp)$/', $name)
    // Some Apache setups run "x.php.zip" as PHP: refuse script extensions anywhere in the name.
    || preg_match('/\.(php[0-9]?|phtml|phar|pht|cgi|pl|py|sh|asp|aspx|jsp|htaccess)(\.|$)/i', $name)) {
    out(400, ['error' => 'nome de arquivo inválido']);
}
$final = $dir . DIRECTORY_SEPARATOR . $name;
$part = $final . '.part';
clearstatcache();

switch ($action) {
    case 'status':
        out(200, [
            'part' => is_file($part) ? filesize($part) : 0,
            'size' => is_file($final) ? filesize($final) : -1,
            'sha256' => (isset($_GET['hash']) && is_file($final)) ? hash_file('sha256', $final) : null,
        ]);

    case 'chunk':
        if ($_SERVER['REQUEST_METHOD'] !== 'POST') {
            out(405, ['error' => 'use POST']);
        }
        $offset = isset($_GET['offset']) ? (int)$_GET['offset'] : -1;
        $current = is_file($part) ? filesize($part) : 0;
        if ($offset !== 0 && $offset !== $current) {
            out(409, ['error' => 'posição inesperada', 'part' => $current]);
        }
        $in = fopen('php://input', 'rb');
        $fp = fopen($part, $offset === 0 ? 'wb' : 'ab');
        if (!$in || !$fp) {
            out(500, ['error' => 'não foi possível gravar']);
        }
        $n = stream_copy_to_stream($in, $fp);
        fclose($fp);
        fclose($in);
        $size = ($offset === 0 ? 0 : $current) + $n;
        if ($size > MAX_FILE_BYTES) {
            @unlink($part);
            out(413, ['error' => 'arquivo grande demais']);
        }
        out(200, ['part' => $size]);

    case 'finish':
        if (!is_file($part)) {
            out(404, ['error' => 'envio não encontrado']);
        }
        $want = strtolower(isset($_GET['sha256']) ? (string)$_GET['sha256'] : '');
        if ($want !== '' && !hash_equals($want, hash_file('sha256', $part))) {
            @unlink($part);
            out(422, ['error' => 'o arquivo chegou corrompido (sha256 não confere)']);
        }
        if (is_file($final)) {
            @unlink($final);
        }
        if (!rename($part, $final)) {
            out(500, ['error' => 'não foi possível finalizar o arquivo']);
        }
        out(200, ['ok' => true, 'size' => filesize($final)]);

    case 'delete':
        if (is_file($final)) {
            @unlink($final);
        }
        out(200, ['ok' => true]);
}

out(400, ['error' => 'ação desconhecida']);
