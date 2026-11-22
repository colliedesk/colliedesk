# Every element the visual editor writes

Plain **bold** *italic* **_both_** ~~struck~~ <u>under</u> <mark>marked</mark> `code` and **bold <u>under bold</u>**.

<span data-color="accent">accent</span> <span data-color="success">success</span> <span data-color="warning">warning</span> <span data-color="danger">danger</span> <span data-color="muted">muted</span> <span data-color="danger"><mark>**all of it**</mark></span>

A [link](https://example.com "A title"), an [article](/kb/articles/getting-started) and [`code in a link`](https://example.com/api).\
After a hard break, an image: ![A screenshot](/api/v1/kb/images/0b6f5c2e-1d3a-4c5b-9e7f-123456789abc "Screenshot")

## Heading two

### Heading three

#### Heading four

##### Heading five

###### Heading six

<p data-align="center">

Centered.

</p>

<p data-align="right" data-indent="2">

Right and indented twice.

</p>

<p data-indent="4">

Indented four times.

</p>

<h2 data-align="center" data-indent="1">

## Centered heading

</h2>

- One
- Two
  - Nested
- Three
  ***

* A list right after another

3. Third
4. Fourth

- [ ] To do
- [x] Done

> A quote.

> [!WARNING]
>
> Deleting a workspace cannot be undone.

```go
fmt.Println("hi")
```

---

| Name | Plan |
| :-: | -: |
| Ana | Team |

<table>
<tr>
<th colspan="2" data-align="center" data-bg="accent">

Merged head

</th>
</tr>
<tr>
<td rowspan="2" data-bg="success">

Tall

</td>
<td data-align="right">

A

</td>
</tr>
<tr>
<td data-bg="muted">

B

- in a cell

</td>
</tr>
</table>

<figure data-width="480" data-align="center" data-border>

[![A screenshot](/api/v1/kb/images/5f0c6a52-8d1e-4b7a-9c3f-2e6d4a1b0c97 "The \"Settings\" page")](https://example.com/guide "Read the guide")

<figcaption>

The **settings** page

</figcaption>

</figure>

<figure data-width="small" data-align="left">

![Chart](/api/v1/kb/images/5f0c6a52-8d1e-4b7a-9c3f-2e6d4a1b0c97 "Sign-ups by week")

</figure>
