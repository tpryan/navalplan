insert into
    "public"."person" (
        "api_key",
        "email",
        "google_id",
        "name",
        "picture_url"
    )
values
    (
        'b097a7e6-24cc-4668-9038-e8e315b6785b',
        'terrence.p.ryan@gmail.com',
        '112829732245606197189',
        'Terrence Ryan',
        'https://lh3.googleusercontent.com/a/ACg8ocKUyj8d4t_GDy7Tj2ZsaOUNcDsRS4nSe0uY_CZ7BWhkDHLV9iXI=s96-c'
    );


UPDATE person SET is_admin = true WHERE email = 'terrence.p.ryan@gmail.com';