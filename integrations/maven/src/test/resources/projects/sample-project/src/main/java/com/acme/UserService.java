package com.acme;

import java.util.ArrayList;
import java.util.List;
import java.util.Optional;

/**
 * Simple user service — the kind of code that would be reviewed by ynh.
 */
public class UserService {

    private final List<User> users = new ArrayList<>();
    private int nextId = 1;

    public User create(String name, String email) {
        if (name == null || name.isBlank()) {
            throw new IllegalArgumentException("name is required");
        }
        if (email == null || email.isBlank()) {
            throw new IllegalArgumentException("email is required");
        }
        User user = new User(nextId++, name, email);
        users.add(user);
        return user;
    }

    public List<User> findAll() {
        return List.copyOf(users);
    }

    public Optional<User> findById(int id) {
        return users.stream().filter(u -> u.id() == id).findFirst();
    }

    public record User(int id, String name, String email) {}
}
