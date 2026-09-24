import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { StripCaptcha, StripCaptchaImage, toAsciiDigits } from '../dist/index.js';

test('toAsciiDigits normalizes other digit scripts and drops the rest', () => {
  assert.equal(toAsciiDigits('١٢٣'), '123');
  assert.equal(toAsciiDigits('۴۵۶'), '456');
  assert.equal(toAsciiDigits('７８９'), '789');
  assert.equal(toAsciiDigits(' 1-2 a3 '), '123');
});

const challenge = {
  id: 'abc',
  image: 'data:image/png;base64,AAAA',
  imageWidth: 300,
  imageHeight: 64,
  width: 204,
  height: 64,
  length: 5,
  expiresIn: 600,
  tiles: [
    [0, 0, 20, 30, 100, 34],
    [20, 0, 184, 30, 0, 34],
    [0, 30, 204, 34, 50, 0],
  ],
};

test('StripCaptchaImage places every tile from its atlas position', () => {
  const html = renderToStaticMarkup(createElement(StripCaptchaImage, { challenge, alt: 'captcha' }));
  // Атлас один раз — в переменной на контейнере, не в каждой плитке
  assert.equal(html.split('data:image/png').length - 1, 1);
  assert.match(html, /--strip-captcha-atlas:url\(&quot;data:image\/png;base64,AAAA&quot;\)/);
  assert.match(html, /left:0;top:0;width:20px;height:30px;[^"]*background-size:300px 64px;background-position:-100px -34px/);
  assert.match(html, /left:20px;top:0;width:184px;height:30px;[^"]*background-position:0px -34px/);
  assert.match(html, /left:0;top:30px;width:204px;height:34px;[^"]*background-position:-50px 0px/);
  assert.match(html, /role="img" aria-label="captcha"/);
});

test('StripCaptcha renders a labelled field with a placeholder while loading', () => {
  const html = renderToStaticMarkup(
    createElement(StripCaptcha, {
      fetchChallenge: async () => challenge,
      onChange: () => {},
      labels: { label: 'Код с картинки', placeholder: 'Введите {n} цифр' },
    }),
  );
  assert.match(html, /strip-captcha--loading/);
  assert.match(html, />Код с картинки</);
  assert.match(html, /placeholder="Введите 5 цифр"/);
  const labelFor = html.match(/<label for="([^"]+)"/)[1];
  assert.match(html, new RegExp(`<input[^>]*id="${labelFor}"`));
});
